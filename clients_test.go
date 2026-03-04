package gomailconn

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	t.Run("nil config returns ErrInvalidConfig", func(t *testing.T) {
		client, err := NewClient(nil)
		if err != ErrInvalidConfig {
			t.Fatalf("NewClient(nil) err = %v, want ErrInvalidConfig", err)
		}
		if client != nil {
			t.Fatal("NewClient(nil) should return nil client")
		}
	})

	t.Run("valid config returns client with Status Init", func(t *testing.T) {
		cfg := &Config{
			Username:   "u",
			Password:   "p",
			IMAPServer: "imap.example.com",
		}
		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient(cfg) err = %v", err)
		}
		if client == nil {
			t.Fatal("NewClient(cfg) should return non-nil client")
		}
		if client.config != cfg {
			t.Fatal("client.config should be the same pointer as cfg")
		}
		if client.Status != ClientStatusInit {
			t.Fatalf("client.Status = %q, want %q", client.Status, ClientStatusInit)
		}
	})
}
