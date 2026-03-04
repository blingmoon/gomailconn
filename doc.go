// Package gomailconn provides IMAP-based mail receiving and SMTP-based sending,
// with parsing of message body and attachments and a callback for new messages.
//
// # Receiving (IMAP)
//
// Configure the client with Config (IMAP server, credentials, mailbox, etc.),
// create a client with NewClient, then call StartWithHandler with a handler
// to listen for new mail in the background (IDLE or polling). Each new message
// is parsed into MailMessage with subject, from, body parts (MailBodyPart) and
// attachments (MailAttachment). Attachments can be saved to a local directory
// (AttachmentDir) with optional size limits (BodyMaxBytes, AttachmentMaxBytes).
// Common charsets such as GBK are supported. If the server does not support
// IDLE, enable ForcedPolling and set CheckInterval for polling.
//
// # Sending (SMTP)
//
// Set Config.SMTPServer (and optionally SMTPPort, SMTPUseTLS), then call
// Client.Send with a SendMailMessage (to, subject, body, optional attachments)
// to send mail via SMTP.
//
// # Basic usage
//
//	cfg := &gomailconn.Config{
//	    Username: "user", Password: "pass",
//	    IMAPServer: "imap.example.com", Mailbox: "INBOX",
//	    SMTPServer: "smtp.example.com",
//	}
//	client, _ := gomailconn.NewClient(cfg)
//	client.StartWithHandler(ctx, func(ctx context.Context, msg *gomailconn.MailMessage) error {
//	    // handle msg body and attachments
//	    return nil
//	})
//	client.Send(ctx, &gomailconn.SendMailMessage{To: "to@example.com", Subject: "Hi", Body: []string{"Hello"}})
//	client.Stop(ctx)
//
// See the example directory for a full runnable example.
package gomailconn
