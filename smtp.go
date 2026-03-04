package gomailconn

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/emersion/go-message/mail"
)

// sanitizeHeaderValue removes CR/LF from s to prevent SMTP header injection.
// go-message textproto also rejects \r\n in header values when writing; we sanitize so the send succeeds.
func sanitizeHeaderValue(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// Send builds and sends an email via SMTP. Caller must set Config.SMTPServer (and optionally SMTPPort, SMTPUseTLS).
func (c *Client) Send(ctx context.Context, msg *SendMailMessage) error {
	if strings.TrimSpace(c.config.SMTPServer) == "" {
		return ErrInvalidConfig
	}

	fromRaw := sanitizeHeaderValue(c.config.Username)
	toRaw := sanitizeHeaderValue(strings.TrimSpace(msg.To))
	if toRaw == "" {
		return ErrInvalidParams
	}

	client, err := c.dialSMTP()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			log.Printf("Send: close smtp client failed: %v", closeErr)
		}
	}()

	// Build message with go-message/mail: RFC-compliant headers via textproto (folding, encoded-words, address list format).
	var h mail.Header
	if fromAddrs, parseErr := mail.ParseAddressList(fromRaw); parseErr == nil && len(fromAddrs) > 0 {
		h.SetAddressList("From", fromAddrs)
	} else {
		h.Set("From", fromRaw)
	}
	if toAddrs, parseErr := mail.ParseAddressList(toRaw); parseErr == nil && len(toAddrs) > 0 {
		h.SetAddressList("To", toAddrs)
	} else {
		h.Set("To", toRaw)
	}
	subject := strings.TrimSpace(msg.Subject)
	if subject == "" {
		subject = "Reply from PicoClaw"
	}
	h.SetSubject(sanitizeHeaderValue(subject))
	h.Set("Content-Type", "text/plain; charset=utf-8")
	var buf bytes.Buffer
	bodyWriter, err := mail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return fmt.Errorf("email build message: %w", err)
	}
	if _, err = bodyWriter.Write([]byte(strings.Join(msg.Body, "\n\n"))); err != nil {
		_ = bodyWriter.Close()
		return fmt.Errorf("email write body: %w", err)
	}
	if err = bodyWriter.Close(); err != nil {
		return fmt.Errorf("email close message: %w", err)
	}
	body := buf.Bytes()

	host := c.config.SMTPServer
	auth := smtp.PlainAuth("", c.config.Username, c.config.Password, host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = client.Mail(fromRaw); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err = client.Rcpt(toRaw); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, dataErr := client.Data()
	if dataErr != nil {
		return fmt.Errorf("smtp data: %w", dataErr)
	}
	if _, err = w.Write(body); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}
	return client.Quit()
}

// dialSMTP connect to SMTP server and return *smtp.Client, caller is responsible for Close.
func (c *Client) dialSMTP() (*smtp.Client, error) {
	port := c.config.SMTPPort
	if port <= 0 {
		if c.config.SMTPUseTLS {
			port = 465
		} else {
			port = 587
		}
	}
	addr := net.JoinHostPort(c.config.SMTPServer, strconv.Itoa(port))
	host := c.config.SMTPServer

	if c.config.SMTPUseTLS {
		// use TLS
		tlsConfig := &tls.Config{ServerName: host}
		conn, tlserr := tls.Dial("tcp", addr, tlsConfig)
		if tlserr != nil {
			return nil, fmt.Errorf("smtp tls dial: %w", tlserr)
		}
		client, newClientErr := smtp.NewClient(conn, host)
		if newClientErr != nil {
			if closeErr := conn.Close(); closeErr != nil {
				log.Printf("dialSMTP: close net.Conn failed: %v, addr: %s", closeErr, addr)
			}
			return nil, fmt.Errorf("smtp new client: %w", newClientErr)
		}
		return client, nil
	}

	// use STARTTLS
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("dialSMTP: close net.Conn failed: %v, addr: %s", closeErr, addr)
		}
		return nil, fmt.Errorf("smtp new client: %w", err)
	}
	if err = client.StartTLS(&tls.Config{ServerName: host}); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("dialSMTP: close net.Conn failed: %v, addr: %s", closeErr, addr)
		}
		return nil, fmt.Errorf("smtp start tls: %w", err)
	}
	return client, nil
}
