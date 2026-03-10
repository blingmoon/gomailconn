# gomailconn

**Other languages:** [简体中文](README.zh-CN.md)

A Go library for **IMAP/SMTP** email: connect to your mailbox, get notified when new mail arrives, and send mail. Built on [go-imap/v2](https://github.com/emersion/go-imap/v2).

- **What it does:** Keeps a long-lived connection to an IMAP server, calls your handler for each new message (with parsed body and attachments), and optionally sends mail via SMTP.
- **Who it’s for:** Services that need to watch a mailbox (e.g. QQ Mail, NetEase 163) or automate read/send without running a full mail client; can be wired into an Agent as a message channel trigger.
- **In one line:** Configure once, run `StartWithHandler`, and handle incoming mail in your code.

## Features

- **IMAP**: Long-lived connection with automatic reconnect and backoff
- **IDLE or polling**: Uses IMAP IDLE when the server supports it; falls back to configurable polling
- **New-mail handler**: Your callback receives parsed `MailMessage` (envelope, body parts, attachments)
- **Attachments**: Optional save to a local directory with size limits; RFC 2047 filename decoding (e.g. GBK)
- **SMTP**: Send mail with optional TLS (port 465) or STARTTLS (port 587)
- **Config**: JSON-friendly struct; can be loaded from env or config files

## Requirements

- Go 1.25.7 or later

## Installation

```bash
go get github.com/blingmoon/gomailconn
```

## Quick start

```go
package main

import (
    "context"
    "log"

    "github.com/blingmoon/gomailconn"
)

func main() {
    cfg := &gomailconn.Config{
        Username:   "your@email.com",
        Password:   "your-password",
        IMAPServer: "imap.example.com",
        IMAPPort:   993,
        UseTLS:     true,
        Mailbox:    "INBOX",
    }

    client, err := gomailconn.NewClient(cfg)
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    err = client.StartWithHandler(ctx, func(ctx context.Context, msg *gomailconn.MailMessage) error {
        log.Printf("New mail: UID=%d From=%q Subject=%q", msg.UID, msg.From, msg.Subject)
        return nil
    })
    if err != nil {
        log.Fatal(err)
    }

    defer client.Stop(ctx)
    // ... wait for shutdown (e.g. signal)
}
```

## Configuration

| Field | Description |
|-------|-------------|
| `Username`, `Password` | Login credentials |
| `IMAPServer`, `IMAPPort` | IMAP host and port (0 = 993 with TLS, 143 without) |
| `UseTLS` | Use TLS for IMAP (default port 993) |
| `Mailbox` | Mailbox name (default `INBOX`) |
| `ConnectWithID` | Send IMAP ID command after login (required for some servers, e.g. 163) |
| `CheckInterval` | Polling interval in seconds when IDLE is not used (default 30) |
| `ForcedPolling` | Disable IDLE and always poll at `CheckInterval` |
| `AttachmentDir` | Directory to save attachments (empty = do not save) |
| `AttachmentMaxBytes`, `BodyMaxBytes` | Size limits (0 = use defaults or no limit) |
| `SMTPServer`, `SMTPPort`, `SMTPUseTLS` | SMTP settings for `Send()` (optional) |

## Sending email

If `SMTPServer` is set, you can send mail:

```go
err := client.Send(ctx, &gomailconn.SendMailMessage{
    To:      "recipient@example.com",
    Subject: "Subject",
    Body:    []string{"Plain text body."},
    // Attachments: []*gomailconn.AttachmentInfo{ ... },
})
```

## Example

A runnable example that loads config from a `.env` file (or env vars) and runs the client until Ctrl+C:

```bash
cd example
cp .env.example .env   # edit with your credentials
go run .
```

From the repository root:

```bash
go run ./example
```

## License

See [LICENSE](LICENSE).
