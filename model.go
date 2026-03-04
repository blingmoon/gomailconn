package gomailconn

import (
	"io"
)

type MailMessage struct {
	UID         uint32
	Subject     string
	From        string
	To          string
	BodyParts   []*MailBodyPart
	Attachments []*MailAttachment
}

type MailAttachment struct {
	AttachmentName     string
	SafeAttachmentName string
	LocalPath          string
	IsParsed           bool
	ParseError         error
}

type MailBodyPart struct {
	ContentType BodyContentType
	Body        string
	IsParsed    bool
	ParseError  error
}

type SendMailMessage struct {
	To          string //required, use "," to separate multiple recipients
	Subject     string
	Body        []string
	Attachments []*AttachmentInfo
}
type AttachmentInfo struct {
	AttachmentName   string
	AttachmentReader io.Reader
}
type BodyContentType = string

const (
	BodyContentTypeText BodyContentType = "text"
	BodyContentTypeHtml BodyContentType = "html"
)
