package auth_test

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/judeotine/afterword/services/api/internal/auth"
)

type otpFixture struct {
	service *auth.OTPService
	store   *memoryOTPStore
	sender  *recordingSender
	now     time.Time
}

func newOTPFixture(t *testing.T) *otpFixture {
	t.Helper()
	fixture := &otpFixture{
		store:  newMemoryOTPStore(),
		sender: &recordingSender{},
		now:    time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC),
	}
	service, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       fixture.store,
		EmailSender: fixture.sender,
		SMSSender:   fixture.sender,
		HashCost:    bcrypt.MinCost,
		Clock:       func() time.Time { return fixture.now },
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	fixture.service = service
	return fixture
}

func (f *otpFixture) send(t *testing.T, channel auth.Channel, destination, ip string) auth.SentCode {
	t.Helper()
	sent, err := f.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     channel,
		Destination: destination,
		RequestIP:   ip,
	})
	if err != nil {
		t.Fatalf("send code: %v", err)
	}
	return sent
}

func (f *otpFixture) lastEmailedCode(t *testing.T) string {
	t.Helper()
	emails := f.sender.sentEmails()
	if len(emails) == 0 {
		t.Fatal("no email was sent")
	}
	return codeFromBody(t, emails[len(emails)-1].Body)
}

func codeFromBody(t *testing.T, body string) string {
	t.Helper()
	code := regexp.MustCompile(`\b\d{6}\b`).FindString(body)
	if code == "" {
		t.Fatalf("no six digit code in %q", body)
	}
	return code
}

func TestOTPSendDeliversASixDigitCodeAndStoresOnlyItsHash(t *testing.T) {
	fixture := newOTPFixture(t)
	sent := fixture.send(t, auth.ChannelEmail, "Person@Example.com", "203.0.113.9")

	if !sent.ExpiresAt.Equal(fixture.now.Add(auth.DefaultOTPTTL)) {
		t.Fatalf("expires at %v, want %v", sent.ExpiresAt, fixture.now.Add(auth.DefaultOTPTTL))
	}

	code := fixture.lastEmailedCode(t)
	if len(code) != 6 {
		t.Fatalf("code %q is not six digits", code)
	}

	records := fixture.store.all()
	if len(records) != 1 {
		t.Fatalf("stored %d records, want 1", len(records))
	}
	record := records[0]
	if record.Destination != "person@example.com" {
		t.Fatalf("destination %q, want the normalised address", record.Destination)
	}
	if strings.Contains(record.CodeHash, code) {
		t.Fatal("the stored hash contains the plaintext code")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(record.CodeHash), []byte(code)); err != nil {
		t.Fatalf("stored hash does not verify the code: %v", err)
	}
	if record.RequestIP != "203.0.113.9" {
		t.Fatalf("request ip %q", record.RequestIP)
	}
}

func TestOTPSendUsesADistinctHashEachTime(t *testing.T) {
	fixture := newOTPFixture(t)
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		fixture.send(t, auth.ChannelEmail, "person@example.com", "")
		fixture.now = fixture.now.Add(11 * time.Minute)
	}
	for _, record := range fixture.store.all() {
		if seen[record.CodeHash] {
			t.Fatal("two codes hashed to the same value")
		}
		seen[record.CodeHash] = true
	}
}

func TestOTPSendUsesTheSMSSenderForPhoneNumbers(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelPhone, "+256700000000", "")

	if len(fixture.sender.sentEmails()) != 0 {
		t.Fatal("an email was sent for a phone destination")
	}
	texts := fixture.sender.sentTexts()
	if len(texts) != 1 {
		t.Fatalf("sent %d texts, want 1", len(texts))
	}
	if texts[0].Destination != "+256700000000" {
		t.Fatalf("destination %q", texts[0].Destination)
	}
}

func TestOTPSendRejectsInvalidDestinations(t *testing.T) {
	fixture := newOTPFixture(t)
	cases := []struct {
		channel     auth.Channel
		destination string
	}{
		{auth.ChannelEmail, ""},
		{auth.ChannelEmail, "not-an-email"},
		{auth.ChannelEmail, "a@b@c.com"},
		{auth.ChannelEmail, strings.Repeat("a", 300) + "@example.com"},
		{auth.ChannelPhone, "0700000000"},
		{auth.ChannelPhone, "+"},
		{auth.ChannelPhone, "+12"},
		{auth.ChannelPhone, "+1800CALLNOW"},
	}
	for _, tc := range cases {
		_, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
			Channel:     tc.channel,
			Destination: tc.destination,
		})
		if !errors.Is(err, auth.ErrInvalidDestination) {
			t.Fatalf("%s %q: got %v, want ErrInvalidDestination", tc.channel, tc.destination, err)
		}
	}
}

func TestOTPSendRejectsAnUnknownChannel(t *testing.T) {
	fixture := newOTPFixture(t)
	_, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.Channel("carrier-pigeon"),
		Destination: "person@example.com",
	})
	if !errors.Is(err, auth.ErrInvalidChannel) {
		t.Fatalf("got %v, want ErrInvalidChannel", err)
	}
}

func TestOTPSendRateLimitsByDestination(t *testing.T) {
	fixture := newOTPFixture(t)
	for i := 0; i < auth.DefaultSendsPerDestination; i++ {
		fixture.send(t, auth.ChannelEmail, "person@example.com", "")
		fixture.now = fixture.now.Add(time.Minute)
	}

	_, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	})
	if !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("got %v, want ErrRateLimited", err)
	}

	if _, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "someone-else@example.com",
	}); err != nil {
		t.Fatalf("another destination should not be limited: %v", err)
	}

	fixture.now = fixture.now.Add(auth.DefaultDestinationWindow)
	if _, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	}); err != nil {
		t.Fatalf("the window should have rolled over: %v", err)
	}
}

func TestOTPSendRateLimitsByIP(t *testing.T) {
	fixture := newOTPFixture(t)
	for i := 0; i < auth.DefaultSendsPerIP; i++ {
		fixture.send(t, auth.ChannelEmail, uniqueEmail(i), "198.51.100.4")
		fixture.now = fixture.now.Add(time.Second)
	}

	_, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: uniqueEmail(999),
		RequestIP:   "198.51.100.4",
	})
	if !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("got %v, want ErrRateLimited", err)
	}

	if _, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: uniqueEmail(1000),
		RequestIP:   "198.51.100.5",
	}); err != nil {
		t.Fatalf("another address should not be limited: %v", err)
	}

	fixture.now = fixture.now.Add(auth.DefaultIPWindow)
	if _, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: uniqueEmail(1001),
		RequestIP:   "198.51.100.4",
	}); err != nil {
		t.Fatalf("the window should have rolled over: %v", err)
	}
}

func TestOTPVerifyAcceptsTheCodeExactlyOnce(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	code := fixture.lastEmailedCode(t)

	verified, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "Person@Example.com",
		Code:        code,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Destination != "person@example.com" || verified.Channel != auth.ChannelEmail {
		t.Fatalf("verified %+v", verified)
	}

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	}); !errors.Is(err, auth.ErrCodeNotFound) {
		t.Fatalf("second use: got %v, want ErrCodeNotFound", err)
	}
}

func TestOTPVerifyRejectsAWrongCodeAndCountsTheAttempt(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	code := fixture.lastEmailedCode(t)

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        wrongCode(code),
	}); !errors.Is(err, auth.ErrCodeMismatch) {
		t.Fatalf("got %v, want ErrCodeMismatch", err)
	}

	records := fixture.store.all()
	if records[0].Attempts != 1 {
		t.Fatalf("attempts %d, want 1", records[0].Attempts)
	}

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	}); err != nil {
		t.Fatalf("the right code should still work: %v", err)
	}
}

func TestOTPVerifyLocksOutAfterTheAttemptLimit(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	code := fixture.lastEmailedCode(t)

	for i := 0; i < auth.DefaultOTPMaxAttempts; i++ {
		if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
			Channel:     auth.ChannelEmail,
			Destination: "person@example.com",
			Code:        wrongCode(code),
		}); !errors.Is(err, auth.ErrCodeMismatch) {
			t.Fatalf("attempt %d: got %v, want ErrCodeMismatch", i+1, err)
		}
	}

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        wrongCode(code),
	}); !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("attempt beyond the limit: got %v, want ErrTooManyAttempts", err)
	}

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	}); !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("the right code after lockout: got %v, want ErrTooManyAttempts", err)
	}
}

func TestOTPVerifyRejectsAnExpiredCode(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	code := fixture.lastEmailedCode(t)

	fixture.now = fixture.now.Add(auth.DefaultOTPTTL)
	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	}); !errors.Is(err, auth.ErrCodeExpired) {
		t.Fatalf("got %v, want ErrCodeExpired", err)
	}
}

func TestOTPVerifyAcceptsACodeInTheLastSecondOfItsLife(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	code := fixture.lastEmailedCode(t)

	fixture.now = fixture.now.Add(auth.DefaultOTPTTL - time.Second)
	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestOTPVerifyOnlyAcceptsTheNewestCode(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	first := fixture.lastEmailedCode(t)

	fixture.now = fixture.now.Add(time.Minute)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")
	second := fixture.lastEmailedCode(t)
	if first == second {
		t.Skip("the two generated codes collided")
	}

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        first,
	}); !errors.Is(err, auth.ErrCodeMismatch) {
		t.Fatalf("superseded code: got %v, want ErrCodeMismatch", err)
	}

	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        second,
	}); err != nil {
		t.Fatalf("newest code: %v", err)
	}
}

func TestOTPVerifyWithNoOutstandingCode(t *testing.T) {
	fixture := newOTPFixture(t)
	if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "nobody@example.com",
		Code:        "123456",
	}); !errors.Is(err, auth.ErrCodeNotFound) {
		t.Fatalf("got %v, want ErrCodeNotFound", err)
	}
}

func TestOTPVerifyRejectsAMalformedCode(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.send(t, auth.ChannelEmail, "person@example.com", "")

	for _, code := range []string{"", "12345", "1234567", "abcdef", " 123456 "} {
		if _, err := fixture.service.Verify(context.Background(), auth.VerifyCodeRequest{
			Channel:     auth.ChannelEmail,
			Destination: "person@example.com",
			Code:        code,
		}); err == nil {
			t.Fatalf("code %q was accepted", code)
		}
	}
	if fixture.store.all()[0].Attempts != 0 {
		t.Fatal("a malformed code should not consume an attempt")
	}
}

func TestOTPSendReportsDeliveryFailures(t *testing.T) {
	fixture := newOTPFixture(t)
	fixture.sender.emailErr = errors.New("smtp is down")

	_, err := fixture.service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	})
	if err == nil {
		t.Fatal("expected the delivery failure to surface")
	}
	if !strings.Contains(err.Error(), "smtp is down") {
		t.Fatalf("error %v does not mention the cause", err)
	}
}

func TestOTPSendRequiresASender(t *testing.T) {
	store := newMemoryOTPStore()
	service, err := auth.NewOTPService(auth.OTPServiceOptions{Store: store, HashCost: bcrypt.MinCost})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	if _, err := service.Send(context.Background(), auth.SendCodeRequest{
		Channel:     auth.ChannelPhone,
		Destination: "+256700000000",
	}); !errors.Is(err, auth.ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

func TestNewOTPServiceRequiresAStore(t *testing.T) {
	if _, err := auth.NewOTPService(auth.OTPServiceOptions{}); err == nil {
		t.Fatal("expected an error when no store is given")
	}
}

func uniqueEmail(n int) string {
	return "person" + strconv.Itoa(n) + "@example.com"
}

func wrongCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}
