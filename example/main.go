package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/blingmoon/gomailconn"
)

func loadDotEnv() {
	const name = ".env"
	b, err := os.ReadFile(name)
	if err != nil {
		return // 文件不存在或不可读就忽略
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		i := strings.Index(line, "=")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		val = strings.Trim(val, `"'`)
		if key != "" {
			os.Setenv(key, val)
		}
	}
}

func main() {
	loadDotEnv()
	// Build config (use env in production to avoid hardcoding secrets)
	cfg := &gomailconn.Config{
		Username:      getEnv("USERNAME", ""),
		Password:      getEnv("PASSWORD", ""),
		IMAPServer:    getEnv("IMAP_SERVER", ""),
		ConnectWithID: strings.ToLower(getEnv("CONNECT_WITH_ID", "false")) == "true",
		Mailbox:       getEnv("MAILBOX", ""),
		ForcedPolling: strings.ToLower(getEnv("FORCED_POLLING", "false")) == "true",
		AttachmentDir: getEnv("ATTACHMENT_DIR", ""),
		SMTPServer:    getEnv("SMTPSERVER", ""),
		UseTLS:        strings.ToLower(getEnv("USE_TLS", "false")) == "true",
		SMTPUseTLS:    strings.ToLower(getEnv("SMTPUSE_TLS", "false")) == "true",
	}
	if getEnv("IMAP_PORT", "0") != "0" {
		cfg.IMAPPort, _ = strconv.Atoi(getEnv("IMAP_PORT", "0"))
	}
	if getEnv("CHECK_INTERVAL", "0") != "0" {
		cfg.CheckInterval, _ = strconv.Atoi(getEnv("CHECK_INTERVAL", "0"))
	}
	if getEnv("ATTACHMENT_MAX_BYTES", "0") != "0" {
		cfg.AttachmentMaxBytes, _ = strconv.ParseInt(getEnv("ATTACHMENT_MAX_BYTES", "0"), 10, 64)
	}
	if getEnv("BODY_MAX_BYTES", "0") != "0" {
		cfg.BodyMaxBytes, _ = strconv.ParseInt(getEnv("BODY_MAX_BYTES", "0"), 10, 64)
	}
	if getEnv("SMTP_PORT", "0") != "0" {
		cfg.SMTPPort, _ = strconv.Atoi(getEnv("SMTP_PORT", "0"))
	}
	if strings.ToLower(getEnv("SMTPUSE_TLS", "false")) == "true" {
		cfg.SMTPUseTLS = true
	}

	client, err := gomailconn.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewClient: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start IMAP: new emails will be delivered to the handler
	handler := func(ctx context.Context, msg *gomailconn.MailMessage) error {
		fmt.Printf("[New Mail] UID=%d From=%q Subject=%q\n", msg.UID, msg.From, msg.Subject)
		for _, p := range msg.BodyParts {
			if p.IsParsed && p.Body != "" {
				fmt.Printf("  Body(%s): %.80s...\n", p.ContentType, p.Body)
			}
		}
		for _, a := range msg.Attachments {
			fmt.Printf("  Attachment: %s (LocalPath=%q)\n", a.AttachmentName, a.LocalPath)
		}
		return nil
	}

	if err := client.StartWithHandler(ctx, handler); err != nil {
		fmt.Fprintf(os.Stderr, "StartWithHandler: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Listening for new mail (Ctrl+C to stop)...")

	// Optional: send a test email if SMTP is configured
	if cfg.SMTPServer != "" {
		go func() {
			time.Sleep(2 * time.Second)
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer sendCancel()
			err := client.Send(sendCtx, &gomailconn.SendMailMessage{
				To:      cfg.Username,
				Subject: "Test from gomailconn example",
				Body:    []string{"This is a test body."},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Send: %v\n", err)
			} else {
				fmt.Println("Test email sent.")
			}
		}()
	}

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("Shutting down...")
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Stop: %v\n", err)
	}
	fmt.Println("Done.")
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
