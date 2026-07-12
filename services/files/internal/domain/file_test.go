package domain

import (
	"errors"
	"testing"
)

func TestValidateUpload(t *testing.T) {
	allowed := map[string]struct{}{"image/png": {}, "application/pdf": {}}
	tests := []struct {
		name   string
		upload Upload
		want   error
	}{
		{"valid attachment", Upload{Name: "report.pdf", ContentType: "application/pdf", Size: 10, Purpose: PurposeAttachment}, nil},
		{"path traversal", Upload{Name: "../secret", ContentType: "application/pdf", Size: 10, Purpose: PurposeAttachment}, ErrInvalidName},
		{"too large", Upload{Name: "x.pdf", ContentType: "application/pdf", Size: 101, Purpose: PurposeAttachment}, ErrFileTooLarge},
		{"denied type", Upload{Name: "x.exe", ContentType: "application/x-msdownload", Size: 10, Purpose: PurposeAttachment}, ErrContentTypeDenied},
		{"avatar is image", Upload{Name: "x.pdf", ContentType: "application/pdf", Size: 10, Purpose: PurposeAvatar}, ErrContentTypeDenied},
		{"purpose required", Upload{Name: "x.png", ContentType: "image/png", Size: 10}, ErrInvalidPurpose},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateUpload(tt.upload, 100, allowed); !errors.Is(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateDetectedType(t *testing.T) {
	if err := ValidateDetectedType("image/png", "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDetectedType("image/png", "image/jpeg"); !errors.Is(err, ErrContentTypeMismatch) {
		t.Fatalf("got %v", err)
	}
}
