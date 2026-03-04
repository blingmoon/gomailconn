package gomailconn

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// extractEmailBodyAndAttachments parses body and saves attachments to AttachmentDir; returns body text and local paths.
func (c *Client) extractEmailBodyAndAttachments(bodyReader imap.LiteralReader) ([]*MailBodyPart, []*MailAttachment, error) {
	if bodyReader == nil {
		return nil, nil, errors.New("body reader is nil")
	}
	var (
		bodyTotalRemainingSize       *int64
		attachmentTotalRemainingSize *int64
	)
	if c.config.BodyMaxBytes > 0 {
		copyBodyTotalRemainingSize := c.config.BodyMaxBytes
		bodyTotalRemainingSize = &copyBodyTotalRemainingSize
	}
	if c.config.AttachmentMaxBytes > 0 {
		copyAttachmentTotalRemainingSize := c.config.AttachmentMaxBytes
		attachmentTotalRemainingSize = &copyAttachmentTotalRemainingSize
	}
	if bodyReader.Size() == 0 {
		return nil, nil, errors.New("body reader size is 0")
	}
	if bodyTotalRemainingSize != nil && attachmentTotalRemainingSize != nil {
		if bodyReader.Size() > *bodyTotalRemainingSize+*attachmentTotalRemainingSize {
			return nil, nil, errors.New("email body and attachment size exceeds limit")
		}
	}
	mr, err := mail.CreateReader(bodyReader)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if closeErr := mr.Close(); closeErr != nil {
			log.Printf("extractEmailBodyAndAttachments: close mail reader failed: %v", closeErr)
		}
	}()

	var bodyParts []*MailBodyPart
	var attachments []*MailAttachment
	attachmentIndex := 0
	saveDir := strings.TrimSpace(c.config.AttachmentDir)

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		contentType := getPartContentType(p.Header)
		innerContentType := covertContentTypeToBodyContentType(contentType)
		isAttachment := isAttachmentPart(p.Header)

		if isAttachment {
			// if attachmentTotalRemainingSize ==nil it means no limit
			if attachmentTotalRemainingSize != nil && *attachmentTotalRemainingSize <= 0 {
				// if attachment limit is exceeded, skip the attachment
				// discard the attachment body
				_, _ = io.Copy(io.Discard, p.Body)
				continue
			}
			filename := getPartFilename(p.Header)
			if filename == "" {
				filename = fmt.Sprintf("attachment_%d", attachmentIndex)
			}
			attachmentIndex++
			// pre check the attachment size
			size, ok := getPartFileSize(p.Header)
			if ok {
				// size is estimated size, may not be accurate
				// if ok check if the attachment size exceeds the limit
				if attachmentTotalRemainingSize != nil && size > *attachmentTotalRemainingSize {
					attachments = append(attachments, &MailAttachment{
						AttachmentName: filename,
						IsParsed:       false,
						ParseError:     errors.New("attachment size exceeds limit, save failed, remaining size: " + fmt.Sprintf("%d", *attachmentTotalRemainingSize)),
					})
					continue
				}
			}

			var localPath string
			var attachmentSize int64
			var saveError error
			if saveDir != "" {

				attachmentSize, localPath, saveError = c.saveAttachmentToLocal(filename, attachmentTotalRemainingSize, p.Body)
				if saveError != nil {
					attachments = append(attachments, &MailAttachment{
						AttachmentName: filename,
						IsParsed:       false,
						ParseError:     saveError,
					})
					continue
				}
				if localPath == "" {
					attachments = append(attachments, &MailAttachment{
						AttachmentName: filename,
						IsParsed:       false,
						ParseError:     errors.New("save failed, local path is empty"),
					})
					continue
				} else {
					attachments = append(attachments, &MailAttachment{
						AttachmentName: filename,
						LocalPath:      localPath,
						IsParsed:       true,
					})
				}
				if attachmentTotalRemainingSize != nil {
					*attachmentTotalRemainingSize = *attachmentTotalRemainingSize - attachmentSize
				}
			} else {
				attachments = append(attachments, &MailAttachment{
					AttachmentName: filename,
					IsParsed:       false,
					ParseError:     errors.New("save directory is not set, save failed"),
				})
				// discard the attachment body
				_, _ = io.Copy(io.Discard, p.Body)

			}
			continue
		}
		if bodyTotalRemainingSize != nil && *bodyTotalRemainingSize <= 0 {
			// if limit is 0, skip the body part
			// discard the body part
			_, _ = io.Copy(io.Discard, p.Body)
			bodyParts = append(bodyParts, &MailBodyPart{
				ContentType: innerContentType,
				Body:        "",
				IsParsed:    false,
				ParseError:  errors.New("body limit exceeded, skipping body part"),
			})
			continue
		}
		if bodyTotalRemainingSize != nil {
			// if limit is set, read the body part with limit
			limitedBody := io.LimitReader(p.Body, *bodyTotalRemainingSize+1)
			body, err := io.ReadAll(limitedBody)
			if err != nil || len(body) == 0 {
				continue
			}
			if len(body) > int(*bodyTotalRemainingSize) {
				bodyParts = append(bodyParts, &MailBodyPart{
					ContentType: innerContentType,
					Body:        "",
					IsParsed:    false,
					ParseError:  errors.New("body limit exceeded, skipping body part"),
				})
				// discard the body part
				_, _ = io.Copy(io.Discard, p.Body)
				continue
			}
			bodyParts = append(bodyParts, &MailBodyPart{
				ContentType: innerContentType,
				Body:        string(body),
				IsParsed:    true,
			})
			*bodyTotalRemainingSize = *bodyTotalRemainingSize - int64(len(body))
			continue
		}
		// if limit is not set, read the body part without limit
		// bodyTotalRemainingSize is nil, so we can read the body part without limit
		body, err := io.ReadAll(p.Body)
		if err != nil || len(body) == 0 {
			continue
		}
		bodyParts = append(bodyParts, &MailBodyPart{
			ContentType: innerContentType,
			Body:        string(body),
			IsParsed:    true,
		})
	}
	return bodyParts, attachments, nil
}

// sanitizeFilename removes potentially dangerous characters from a filename
// and returns a safe version for local filesystem storage.
func sanitizeFilename(filename string) string {
	base := filepath.Base(filename)
	// Remove path separators and ".."
	base = strings.ReplaceAll(base, "..", "")
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")
	// Strip null and other control characters (0x00-0x1f, 0x7f)
	base = strings.Map(func(r rune) rune {
		if r == 0 || r == '/' || r == '\\' || r < 32 || r == 127 {
			return -1
		}
		return r
	}, base)
	// Avoid reserved/all-dots names; empty will be replaced by caller with "attachment"
	if strings.Trim(base, ".") == "" {
		return ""
	}
	// Some filesystems limit filename length (e.g. 255 bytes)
	const maxBaseLen = 200
	if len(base) > maxBaseLen {
		ext := filepath.Ext(base)
		base = base[:maxBaseLen-len(ext)] + ext
	}
	return base
}

// saveAttachmentToLocal writes the attachment stream to AttachmentDir with size limit; returns local path or empty on failure or if over limit.
// return the size of the attachment and the local path
func (c *Client) saveAttachmentToLocal(filename string, limit *int64, r io.Reader) (int64, string, error) {
	dir := strings.TrimSpace(c.config.AttachmentDir)
	if dir == "" {
		// if save directory is not set, io discard the attachment
		return 0, "", errors.New("save directory is not set")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		// if failed to create directory, io discard the attachment
		_, _ = io.Copy(io.Discard, r)
		return 0, "", errors.New("failed to create directory")
	}
	safeName := sanitizeFilename(filename)
	if safeName == "" {
		safeName = "attachment"
	}
	ext := filepath.Ext(safeName)
	localName := fmt.Sprintf("%s%s", strings.TrimSuffix(safeName, ext), ext)
	localPath := filepath.Join(dir, localName)
	// check if the file exists
	if _, err := os.Stat(localPath); err == nil {
		// file exists,  add uuid to the filename
		localName = fmt.Sprintf("%s_%s%s", uuid.New().String(), strings.TrimSuffix(safeName, ext), ext)
		localPath = filepath.Join(dir, localName)
	}
	f, err := os.Create(localPath)
	if err != nil {
		return 0, "", errors.New("failed to create attachment file")
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Printf("saveAttachmentToLocal: close attachment file failed: %v, local path: %s", closeErr, localPath)
		}
	}()
	// +1 to detect if the attachment exceeds the limit
	if limit != nil && *limit > 0 {
		limited := io.LimitReader(r, *limit+1)
		n, err := io.Copy(f, limited)
		if err != nil {
			return 0, "", errors.New("failed to copy attachment")
		}
		if n > *limit {
			_ = os.Remove(localPath)
			return 0, "", errors.New("attachment exceeds size limit")
		}
		return n, localPath, nil
	}
	// if limit is not set, copy the attachment to the local file
	n, err := io.Copy(f, r)
	if err != nil {
		return 0, "", errors.New("failed to copy attachment")
	}
	return n, localPath, nil
}

// getPartFilename gets the attachment filename from MIME part header and decodes RFC 2047 (e.g. =?GBK?Q?...?=) to UTF-8.
func getPartFilename(h mail.PartHeader) string {
	if h == nil {
		return ""
	}
	var raw string
	if ah, ok := h.(*mail.AttachmentHeader); ok {
		s, _ := ah.Filename()
		raw = strings.TrimSpace(s)
	} else {
		disp := h.Get("Content-Disposition")
		if disp == "" {
			return ""
		}
		raw = parseFilenameFromDisposition(disp)
	}
	if raw == "" {
		return ""
	}
	return decodeRFC2047Filename(raw)
}

func getPartFileSize(h mail.PartHeader) (int64, bool) {
	if h == nil {
		return 0, false
	}
	disp := h.Get("Content-Disposition")
	if disp == "" {
		return 0, false
	}
	raw := parseFilenameFromDisposition(disp)
	if raw == "" {
		return 0, false
	}
	return getPartFileSizeFromDisposition(disp)
}

func getPartFileSizeFromDisposition(disp string) (int64, bool) {
	dispLower := strings.ToLower(disp)
	if !strings.Contains(dispLower, "attachment") && !strings.Contains(dispLower, "inline") {
		return 0, false
	}
	const fn = "size="
	i := strings.Index(dispLower, fn)
	if i < 0 {
		return 0, false
	}
	disp = disp[i+len(fn):]
	disp = strings.TrimSpace(disp)
	if disp == "" {
		return 0, false
	}
	size, err := strconv.ParseInt(disp, 10, 64)
	if err != nil {
		return 0, false
	}
	return size, true
}

// parseFilenameFromDisposition parses the filename= value from Content-Disposition header.
func parseFilenameFromDisposition(disp string) string {
	dispLower := strings.ToLower(disp)
	if !strings.Contains(dispLower, "attachment") && !strings.Contains(dispLower, "inline") {
		return ""
	}
	const fn = "filename="
	i := strings.Index(dispLower, fn)
	if i < 0 {
		return ""
	}

	disp = disp[i+len(fn):]
	disp = strings.TrimLeft(disp, " \t")
	if len(disp) >= 2 && (disp[0] == '"' || disp[0] == '\'') {
		end := strings.IndexByte(disp[1:], disp[0])
		if end >= 0 {
			return strings.TrimSpace(disp[1 : 1+end])
		}
	}
	if idx := strings.IndexAny(disp, " \t;"); idx > 0 {
		disp = disp[:idx]
	}
	return strings.TrimSpace(disp)
}

// rfc2047WordDecoder decodes =?charset?Q?encoded?= to UTF-8; supports GBK/GB2312.
var rfc2047WordDecoder = &mime.WordDecoder{
	CharsetReader: func(charset string, r io.Reader) (io.Reader, error) {
		charset = strings.ToLower(strings.TrimSpace(charset))
		switch charset {
		case "gbk", "gb2312":
			return simplifiedchinese.GBK.NewDecoder().Reader(r), nil
		default:
			return r, nil
		}
	},
}

func decodeRFC2047Filename(s string) string {
	if s == "" || !strings.Contains(s, "=?") {
		return s
	}
	decoded, err := rfc2047WordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return strings.TrimSpace(decoded)
}

// getPartContentType returns the Content-Type main type (e.g. "text/plain") from PartHeader.
func getPartContentType(h mail.PartHeader) string {
	if h == nil {
		return ""
	}
	raw := h.Get("Content-Type")
	if raw == "" {
		return ""
	}
	// Take the part before the first semicolon and trim
	if i := strings.IndexByte(raw, ';'); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(strings.ToLower(raw))
}

func covertContentTypeToBodyContentType(ct string) BodyContentType {
	if strings.HasPrefix(ct, "text/html") {
		return BodyContentTypeHtml
	}
	if strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/") {
		return BodyContentTypeText
	}
	return ""
}

// isAttachmentPart reports whether the part should be treated as an attachment (not shown as body).
func isAttachmentPart(h mail.PartHeader) bool {
	if h == nil {
		return false
	}
	if _, ok := h.(*mail.AttachmentHeader); ok {
		return true
	}
	disp := strings.ToLower(strings.TrimSpace(h.Get("Content-Disposition")))
	if strings.HasPrefix(disp, "attachment") {
		return true
	}
	ct := getPartContentType(h)
	// Non-text/* (e.g. image, PDF) is treated as attachment
	if ct != "" && !strings.HasPrefix(ct, "text/") {
		return true
	}
	return false
}
