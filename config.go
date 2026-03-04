package gomailconn

type Config struct {
	// basic auth
	Username string `json:"username"`
	Password string `json:"password"`

	// listen config
	IMAPServer string `json:"imap_server"`
	IMAPPort   int    `json:"imap_port"`
	// if true, after login, execute ID COMMAND
	// some email server require this command to be executed to get the mailbox info
	// eg: 163.com
	ConnectWithID bool   `json:"connect_with_id"`
	Mailbox       string `json:"mailbox"` // default "INBOX"
	// seconds, default 30s; polling when IDLE disabled, if not configured, use default 30s
	CheckInterval int  `json:"check_interval"`
	UseTLS        bool `json:"use_tls"`
	// ForcedPolling: when the mail server does not implement IDLE/NOOP per spec,
	// set true to use app-level polling at CheckInterval.
	ForcedPolling bool `json:"forced_polling"`
	// if not configured, will not save attachments to the local filesystem
	AttachmentDir string `json:"attachment_dir"`
	// all attachments in the email will be saved to the attachment directory,
	// if not configured, use no limit, 0 = use default
	AttachmentMaxBytes int64 `json:"attachment_max_bytes"`
	// max size per body part (text/plain, text/html) to avoid unbounded io.ReadAl,
	// if not configured, use no limit, 0 = use default
	BodyMaxBytes int64 `json:"body_max_bytes"`

	// SMTP send (optional, if not configured, Send is not available)
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"` // if not configured, use default port 465 or 587
	// 465  use true, 587  use false+STARTTLS
	SMTPUseTLS bool `json:"smtp_use_tls"`
}
