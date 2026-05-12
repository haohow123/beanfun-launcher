package beanfun

import "testing"

func TestLoginService_StartQRLogin(t *testing.T) {
	t.Parallel()

	s := NewLoginService()
	got, err := s.StartQRLogin()
	if err != nil {
		t.Fatalf("StartQRLogin: unexpected error %v", err)
	}
	if got.BitmapBase64 == "" {
		t.Error("BitmapBase64 is empty")
	}
	if got.Deeplink == "" {
		t.Error("Deeplink is empty")
	}
}

func TestLoginService_CheckQRLogin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// call is the 1-based index of CheckQRLogin invocations after
		// a fresh StartQRLogin.
		call int
		want QRStatus
	}{
		{name: "first poll is pending", call: 1, want: QRStatusPending},
		{name: "second poll is pending", call: 2, want: QRStatusPending},
		{name: "third poll flips to approved", call: 3, want: QRStatusApproved},
		{name: "stays approved after threshold", call: 4, want: QRStatusApproved},
	}

	s := NewLoginService()
	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.CheckQRLogin()
			if err != nil {
				t.Fatalf("CheckQRLogin: %v", err)
			}
			if got != tt.want {
				t.Errorf("call %d: got %q, want %q", tt.call, got, tt.want)
			}
		})
	}
}

func TestLoginService_StartQRLogin_ResetsPolls(t *testing.T) {
	t.Parallel()

	s := NewLoginService()
	_, _ = s.StartQRLogin()
	// Drain past the approval threshold.
	for range 5 {
		_, _ = s.CheckQRLogin()
	}
	// Restarting should send us back to pending.
	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	if got != QRStatusPending {
		t.Errorf("expected restart to reset to pending, got %q", got)
	}
}
