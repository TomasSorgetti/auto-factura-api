package auth

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

const (
	totpPeriod     = 30
	totpDigits     = 6
	totpIssuer     = "Auto Factura"
	recoveryCount  = 10
	challengeTTL   = 5 * time.Minute
	challengeTries = 5
)

// TOTPSetup is shown once, while the user enrolls an authenticator app.
type TOTPSetup struct {
	Secret     logx.Secret
	OtpauthURL logx.Secret
	QRCodePNG  string
}

func newTOTPKey(email string) (*otp.Key, error) {
	k, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: email,
		Period:      totpPeriod,
		SecretSize:  20,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, fmt.Errorf("generate totp key: %w", err)
	}
	return k, nil
}

func qrDataURL(k *otp.Key) (string, error) {
	img, err := k.Image(256, 256)
	if err != nil {
		return "", fmt.Errorf("render qr: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode qr: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// matchTOTP returns the time step that matches code within ±1 step of now.
func matchTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return 0, false
		}
	}
	opts := totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	cur := now.Unix() / totpPeriod
	for s := cur - 1; s <= cur+1; s++ {
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), opts)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			step, ok = s, true
		}
	}
	return step, ok
}
