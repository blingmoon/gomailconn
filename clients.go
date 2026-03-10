package gomailconn

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	charset "github.com/emersion/go-message/charset"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func init() {
	// Register GBK so go-message can decode mail body (e.g. QQ/163 mailboxes); otherwise "unhandled charset \"gbk\"".
	charset.RegisterEncoding("gbk", simplifiedchinese.GBK)
}

const (
	// reconnect backoff initial
	reconnectBackoffInitial = 1 * time.Second
	// reconnect backoff max
	reconnectBackoffMax = 10 * time.Minute
)

type Client struct {
	config        *Config
	imapClient    *imapclient.Client
	lastUID       uint32
	idleUpdatesCh chan struct{}
	// checkEmailMutex
	// maybe multiple goroutine check email at the same time, use mutex to avoid duplicate check
	checkEmailMutex sync.Mutex

	mu          sync.Mutex
	cancel      context.CancelFunc
	checkTicker *time.Ticker
	// loopWg waits for checkLoop goroutine to exit in Stop().
	loopWg sync.WaitGroup
	// reconnect control
	//
	// reconnectClientVersion is an atomic counter incremented on every successful reconnect.
	// Before acquiring reconnectMutex a goroutine snapshots the counter; after acquiring
	// the lock it re-reads and compares: if the value is unchanged the goroutine is the
	// first to hold the lock since the connection broke, so it performs the reconnect;
	// if the value has changed another goroutine already reconnected, so it exits early.
	// Using atomic.Int64 makes the pre-lock Load() race-free.
	reconnectClientVersion atomic.Int64
	reconnectMutex         sync.Mutex
	status                 ClientStatus
	parserHandler          func(ctx context.Context, mailMessage *MailMessage) error

	dealMailMessageCh chan *MailMessage
}

func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}
	return &Client{
		config: cfg,
		status: ClientStatusInit,
	}, nil
}

func (c *Client) StartWithHandler(ctx context.Context, handler func(ctx context.Context, mailMessage *MailMessage) error) error {
	if c.config.IMAPServer == "" || c.config.Username == "" || c.config.Password == "" {
		return ErrInvalidConfig
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.status != ClientStatusInit {
		c.mu.Unlock()
		// cancel the context
		cancel()
		return ErrAlreadyInitialized
	}
	// change status to initing, avoid duplicate start
	c.status = ClientStatusIniting
	c.cancel = cancel
	c.parserHandler = handler
	c.idleUpdatesCh = make(chan struct{}, 1)
	queueSize := c.config.MailQueueSize
	if queueSize <= 0 {
		queueSize = DefaultMailQueueSize
	}
	c.dealMailMessageCh = make(chan *MailMessage, queueSize)
	c.mu.Unlock()

	// Channel for IDLE unilateral updates (new mail); connect() will set UnilateralDataHandler to send here.
	if err := c.connect(); err != nil {
		cancel()
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}
	// change status to running
	c.mu.Lock()
	c.status = ClientStatusRunning
	c.mu.Unlock()

	c.loopWg.Add(1)
	go c.checkLoop(runCtx)

	// consumer the mail message
	c.loopWg.Add(1)
	go c.consumerMailMessage(runCtx)
	return nil
}

func (c *Client) consumerMailMessage(ctx context.Context) {
	defer c.loopWg.Done()
	if c.dealMailMessageCh == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case mailMessage := <-c.dealMailMessageCh:
			if err := c.parserHandler(ctx, mailMessage); err != nil {
				log.Printf("consumerMailMessage failed to deal mail message: %v", err)
			}
		}
	}
}

func (c *Client) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.checkTicker != nil {
		c.checkTicker.Stop()
		c.checkTicker = nil
	}
	imapClient := c.imapClient
	c.imapClient = nil
	c.mu.Unlock()
	if imapClient != nil {
		_ = imapClient.Logout().Wait()
	}

	c.loopWg.Wait() // wait for checkLoop goroutine to exit

	c.mu.Lock()
	c.status = ClientStatusStopped
	c.mu.Unlock()
	return nil
}

func (c *Client) connect() error {
	port := c.config.IMAPPort
	if port <= 0 {
		if c.config.UseTLS {
			port = 993
		} else {
			port = 143
		}
	}
	address := fmt.Sprintf("%s:%d", c.config.IMAPServer, port)

	opts := &imapclient.Options{
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil && c.idleUpdatesCh != nil {
					select {
					case c.idleUpdatesCh <- struct{}{}:
					default:
					}
				}
			},
		},
	}

	var cl *imapclient.Client
	var err error
	if c.config.UseTLS {
		cl, err = imapclient.DialTLS(address, opts)
	} else {
		cl, err = imapclient.DialInsecure(address, opts)
	}
	if err != nil {
		return err
	}

	if loginErr := cl.Login(c.config.Username, c.config.Password).Wait(); loginErr != nil {
		_ = cl.Logout().Wait()
		return loginErr
	}

	c.mu.Lock()
	c.imapClient = cl
	c.mu.Unlock()

	mailbox := c.config.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if c.config.ConnectWithID {
		// check if the server support ID COMMAND
		caps, err := cl.Capability().Wait()
		if err != nil {
			return fmt.Errorf("failed to get capabilities: %w", err)
		}
		if !caps.Has(imap.CapID) {
			return fmt.Errorf("ID COMMAND not supported, please set connect_with_id to false")
		}
		_, idErr := cl.ID(&imap.IDData{Name: "gomailconn", Version: "1.0"}).Wait()
		if idErr != nil {
			return fmt.Errorf("failed to execute ID COMMAND: %w", idErr)
		}
	}
	selectCmd := cl.Select(mailbox, &imap.SelectOptions{ReadOnly: false})
	selectData, err := selectCmd.Wait()
	if err != nil {
		return fmt.Errorf("failed to select mailbox %s: %w", mailbox, err)
	}

	if selectData != nil && selectData.UIDNext > 0 {
		c.mu.Lock()
		if c.lastUID == 0 {
			c.lastUID = uint32(selectData.UIDNext) - 1
		}
		c.mu.Unlock()
	} else {
		if err := c.syncLastUID(cl); err != nil {
			_ = cl.Logout().Wait()
			return fmt.Errorf("failed to sync mailbox UID: %w", err)
		}
	}
	return nil
}

// syncLastUID fetches the mailbox max UID and sets lastUID so only mail after connect is processed.
func (c *Client) syncLastUID(cl *imapclient.Client) error {
	c.mu.Lock()
	if c.lastUID != 0 {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	criteria := &imap.SearchCriteria{}
	data, err := cl.UIDSearch(criteria, nil).Wait()
	if err != nil {
		var all imap.UIDSet
		all.AddRange(imap.UID(1), 0)
		criteria.UID = []imap.UIDSet{all}
		data, err = cl.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return err
		}
	}
	uids := data.AllUIDs()
	var maxUID uint32
	for _, uid := range uids {
		if uint32(uid) > maxUID {
			maxUID = uint32(uid)
		}
	}
	c.mu.Lock()
	if c.lastUID == 0 {
		c.lastUID = maxUID
	}
	c.mu.Unlock()
	return nil
}

// closeIMAPClient logs out and clears the current IMAP client. Caller must not hold c.mu.
func (c *Client) closeIMAPClient() {
	c.mu.Lock()
	cl := c.imapClient
	c.imapClient = nil
	c.mu.Unlock()
	if cl != nil {
		_ = cl.Logout().Wait()
	}
}

// reconnectWithBackoff closes the current IMAP client and reconnects with exponential backoff until success or ctx is done.
// At most one goroutine performs the actual reconnect; the rest detect the version bump and exit early.
func (c *Client) reconnectWithBackoff(ctx context.Context) error {
	// Snapshot the version atomically BEFORE acquiring the mutex.
	// This read is always race-free because reconnectClientVersion is an atomic.Int64.
	currentClientVersion := c.reconnectClientVersion.Load()

	c.reconnectMutex.Lock()
	defer c.reconnectMutex.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Re-read the version under the mutex.
	// If it differs from our snapshot, another goroutine incremented it and already
	// performed a reconnect while we were waiting — no need to reconnect again.
	if c.reconnectClientVersion.Load() != currentClientVersion {
		c.mu.Lock()
		isOk := c.imapClient != nil && c.imapClient.State() == imap.ConnStateSelected
		c.mu.Unlock()
		if isOk {
			return nil
		}
		// Version changed but client is still broken; fall through and reconnect anyway.
	}

	c.closeIMAPClient()
	backoff := reconnectBackoffInitial
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := c.connect()
		if err == nil {
			// Increment only on success so goroutines still waiting on the mutex
			// can distinguish "reconnect succeeded" from "reconnect failed".
			c.reconnectClientVersion.Add(1)
			return nil
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			if backoff < reconnectBackoffMax {
				backoff *= 2
				if backoff > reconnectBackoffMax {
					backoff = reconnectBackoffMax
				}
			}
		}
	}
}

func (c *Client) checkLoop(ctx context.Context) {
	defer c.loopWg.Done()
	interval := time.Duration(c.config.CheckInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	// Run one check immediately
	c.checkNewEmails(ctx)
	// support IDLE user idle loop, waiting for server push update
	isSupportIDLE := false
	c.mu.Lock()
	cl := c.imapClient
	c.mu.Unlock()
	if cl == nil {
		return
	}
	// check is support IDLE
	caps, err := cl.Capability().Wait()
	if err != nil {
		isSupportIDLE = false
	} else {
		if caps.Has(imap.CapIdle) {
			isSupportIDLE = true
		}
	}

	if !c.config.ForcedPolling {
		if isSupportIDLE {
			c.runIdleLoop(ctx)
			return
		}
	}

	// not support IDLE user polling mode, check new emails every interval
	c.mu.Lock()
	c.checkTicker = time.NewTicker(interval)
	ticker := c.checkTicker
	c.mu.Unlock()
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkNewEmails(ctx)
		}
	}
}

// runIdleLoop uses IMAP IDLE (RFC 2177). When the server pushes a mailbox update (e.g. * EXISTS for new mail),
// UnilateralDataHandler.Mailbox sends to idleUpdatesCh; we close the Idle command and run checkNewEmails().
func (c *Client) runIdleLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.mu.Lock()
		cl := c.imapClient
		c.mu.Unlock()
		if cl == nil {
			return
		}
		if cl.State() != imap.ConnStateSelected {
			if err := c.reconnectWithBackoff(ctx); err != nil {
				log.Printf("runIdleLoop: reconnect after state check failed: %v", err)
				return
			}
			continue
		}
		idleCmd, err := cl.Idle()
		if err != nil {
			if err := c.reconnectWithBackoff(ctx); err != nil {
				log.Printf("runIdleLoop: reconnect after Idle failed: %v", err)
				return
			}
			continue
		}
		done := make(chan error, 1)
		go func() { done <- idleCmd.Wait() }()
		select {
		case <-ctx.Done():
			_ = idleCmd.Close()
			<-done
			return
		case <-c.idleUpdatesCh:
			_ = idleCmd.Close()
			if err := <-done; err != nil {
				if err := c.reconnectWithBackoff(ctx); err != nil {
					log.Printf("runIdleLoop: reconnect after Idle close failed: %v", err)
					return
				}
			}
			c.checkNewEmails(ctx)
		case err := <-done:
			if err != nil {
				if err := c.reconnectWithBackoff(ctx); err != nil {
					log.Printf("runIdleLoop: reconnect after Idle wait failed: %v", err)
					return
				}
			}
			c.checkNewEmails(ctx)
		}
	}
}

func (c *Client) checkNewEmails(ctx context.Context) {
	// the lock is to avoid duplicate check email, maybe multiple goroutine check email at the same time
	c.checkEmailMutex.Lock()
	defer c.checkEmailMutex.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		c.mu.Lock()
		cl := c.imapClient
		lastUID := c.lastUID
		c.mu.Unlock()

		if cl == nil {
			return
		}

		// Check connection state; reconnect with backoff if needed
		if cl.State() != imap.ConnStateSelected {
			if err := c.reconnectWithBackoff(ctx); err != nil {
				log.Printf("checkNewEmails: reconnect after state check failed: %v", err)
				return
			}
			continue
		}

		// Only process mail after recorded lastUID (search by UID range, not by unread)
		criteria := &imap.SearchCriteria{
			NotFlag: []imap.Flag{imap.FlagSeen},
		}
		if lastUID > 0 {
			var uidSet imap.UIDSet
			uidSet.AddRange(imap.UID(lastUID+1), 0)
			criteria.UID = []imap.UIDSet{uidSet}
		}

		searchData, err := cl.UIDSearch(criteria, nil).Wait()
		if err != nil {
			c.closeIMAPClient()
			if reconnectErr := c.reconnectWithBackoff(ctx); reconnectErr != nil {
				log.Printf("checkNewEmails: reconnect after UIDSearch failed: %v", reconnectErr)
				return
			}
			continue
		}

		searchUids := searchData.AllUIDs()
		uids := make([]imap.UID, 0, len(searchUids))
		for _, uid := range searchUids {
			if uid > imap.UID(lastUID) {
				uids = append(uids, uid)
			}
		}
		if len(uids) == 0 {
			return
		}

		maxUID, err := c.streamFetchEmail(ctx, cl, uids)
		if err != nil {
			return
		}

		// Update last processed UID
		if maxUID > 0 {
			c.mu.Lock()
			if c.lastUID < maxUID {
				c.lastUID = maxUID
			}
			c.mu.Unlock()
		}
		return
	}
}

func (c *Client) streamFetchEmail(ctx context.Context, cl *imapclient.Client, uids []imap.UID) (uint32, error) {
	fetchSet := imap.UIDSetNum(uids...)
	bodySection := &imap.FetchItemBodySection{}
	fetchOptions := &imap.FetchOptions{
		Envelope:    true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
		Flags:       true,
	}
	fetchCmd := cl.Fetch(fetchSet, fetchOptions)
	defer func() {
		if closeErr := fetchCmd.Close(); closeErr != nil {
			log.Printf("streamFetchEmail: close fetch command failed: %v, uid: %v", closeErr, uids)
		}
	}()
	maxUID := uint32(0)
	for {
		mailMessage := fetchCmd.Next()
		if mailMessage == nil {
			break
		}
		mailInfo, err := c.parseEmail(ctx, mailMessage)
		if err != nil {
			return 0, err
		}
		// update the max UID
		if mailInfo.UID > maxUID {
			maxUID = mailInfo.UID
		}
		if c.dealMailMessageCh != nil {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case c.dealMailMessageCh <- mailInfo:
			}
		}

		// mark the email as seen
		seenSet := imap.UIDSetNum(imap.UID(mailInfo.UID))
		storeCmd := cl.Store(seenSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Flags:  []imap.Flag{imap.FlagSeen},
			Silent: true,
		}, nil)
		_ = storeCmd.Close()
	}
	return maxUID, nil
}

func (c *Client) parseEmail(ctx context.Context, mailMessage *imapclient.FetchMessageData) (*MailMessage, error) {
	if mailMessage == nil {
		return nil, errors.New("mail message is nil")
	}
	var (
		envelope    *imap.Envelope
		uid         imap.UID
		bodyParts   []*MailBodyPart
		attachments []*MailAttachment
		parsedError error
	)
	for {
		item := mailMessage.Next()
		if item == nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		switch i := item.(type) {
		case imapclient.FetchItemDataEnvelope:
			envelope = i.Envelope
		case imapclient.FetchItemDataUID:
			uid = i.UID
		case imapclient.FetchItemDataBodySection:
			// Must read the literal immediately: go-imap v2 blocks the parser until
			// the literal is consumed; otherwise Next() deadlocks and body stays empty.
			if i.Literal == nil {
				continue
			}
			bodyParts, attachments, parsedError = c.extractEmailBodyAndAttachments(i.Literal)
		default:
		}
	}
	if parsedError != nil {
		return nil, parsedError
	}

	// Extract sender
	senderID := ""
	if len(envelope.From) > 0 {
		from := envelope.From[0]
		if from.Mailbox != "" {
			senderID = fmt.Sprintf("%s@%s", from.Mailbox, from.Host)
		}
	}
	if senderID == "" {
		senderID = "unknown"
	}
	toID := ""
	if len(envelope.To) > 0 {
		to := envelope.To[0]
		if to.Mailbox != "" {
			toID = fmt.Sprintf("%s@%s", to.Mailbox, to.Host)
		}
	}
	if toID == "" {
		toID = "unknown"
	}
	mailInfo := &MailMessage{
		UID:         uint32(uid),
		Subject:     envelope.Subject,
		From:        senderID,
		To:          toID,
		BodyParts:   bodyParts,
		Attachments: attachments,
	}

	return mailInfo, nil
}
