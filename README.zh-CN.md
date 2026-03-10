# gomailconn

**其他语言 / Other languages:** [English](README.md)

基于 [go-imap/v2](https://github.com/emersion/go-imap/v2) 的 **IMAP/SMTP** Go 库：连接邮箱、新邮件到达时回调、并可发信。

- **做什么：** 与 IMAP 服务器保持长连接，每封新邮件触发你的回调（含解析后的正文与附件），并可选的通过 SMTP 发信。
- **适用场景：** 需要监听邮箱的服务（如 QQ 邮箱、网易 163）或做收/发自动化的程序，可直接接入 Agent 作为一个消息通道触发器，无需跑完整邮件客户端。
- **一句话：** 配好 Config，调用 `StartWithHandler`，在代码里处理新邮件即可。

## 功能特性

- **IMAP**：长连接，断线自动重连与退避
- **IDLE 或轮询**：服务端支持则用 IMAP IDLE，否则按配置间隔轮询
- **新邮件回调**：回调中收到解析后的 `MailMessage`（信封、正文、附件）
- **附件**：可选保存到本地目录并限制大小；支持 RFC 2047 文件名解码（如 GBK）
- **SMTP**：支持 TLS（465）或 STARTTLS（587）发信
- **配置**：支持 JSON 的配置结构体，可从环境变量或配置文件加载

## 环境要求

- Go 1.25.7 及以上

## 安装

```bash
go get github.com/blingmoon/gomailconn
```

## 快速开始

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
    // ... 等待退出（如监听信号）
}
```

## 配置说明

| 字段 | 说明 |
|------|------|
| `Username`, `Password` | 登录账号与密码 |
| `IMAPServer`, `IMAPPort` | IMAP 地址与端口（0 表示 TLS 时 993，非 TLS 时 143） |
| `UseTLS` | IMAP 是否使用 TLS（默认 993） |
| `Mailbox` | 邮箱目录名（默认 `INBOX`） |
| `ConnectWithID` | 登录后是否发送 IMAP ID 命令（部分服务器如 163 需要） |
| `CheckInterval` | 未使用 IDLE 时的轮询间隔（秒，默认 30） |
| `ForcedPolling` | 是否禁用 IDLE，始终按 `CheckInterval` 轮询 |
| `AttachmentDir` | 附件保存目录（为空则不保存） |
| `AttachmentMaxBytes`, `BodyMaxBytes` | 附件/正文大小限制（0 表示默认或无限制） |
| `SMTPServer`, `SMTPPort`, `SMTPUseTLS` | SMTP 发信配置（可选） |

## 发送邮件

配置了 `SMTPServer` 后，可调用发信：

```go
err := client.Send(ctx, &gomailconn.SendMailMessage{
    To:      "recipient@example.com",
    Subject: "Subject",
    Body:    []string{"Plain text body."},
    // Attachments: []*gomailconn.AttachmentInfo{ ... },
})
```

## 示例

仓库内提供可运行示例，从 `.env`（或环境变量）读取配置，运行后按 Ctrl+C 退出：

```bash
cd example
cp .env.example .env   # 填入你的账号信息
go run .
```

或在仓库根目录执行：

```bash
go run ./example
```

## 许可证

见 [LICENSE](LICENSE)。
