package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelPhone Channel = "phone"
)

func (c Channel) Valid() bool {
	return c == ChannelEmail || c == ChannelPhone
}

func ParseChannel(value string) (Channel, error) {
	channel := Channel(strings.ToLower(strings.TrimSpace(value)))
	if !channel.Valid() {
		return "", ErrInvalidChannel
	}
	return channel, nil
}

const (
	DefaultOTPTTL              = 10 * time.Minute
	DefaultOTPMaxAttempts      = 5
	DefaultSendsPerDestination = 3
	DefaultDestinationWindow   = 10 * time.Minute
	DefaultSendsPerIP          = 30
	DefaultIPWindow            = time.Hour

	DefaultVerifiesPerDestination  = 10
	DefaultVerifyDestinationWindow = 10 * time.Minute
	DefaultVerifiesPerIP           = 60
	DefaultVerifyIPWindow          = time.Hour

	otpCodeDigits      = 6
	maxDestinationSize = 254
)

var (
	codePattern  = regexp.MustCompile(`^[0-9]{6}$`)
	phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	codeCeiling  = big.NewInt(1_000_000)
)

type OTPRecord struct {
	ID          uuid.UUID
	Channel     Channel
	Destination string
	CodeHash    string
	ExpiresAt   time.Time
	Attempts    int32
	ConsumedAt  *time.Time
	RequestIP   string
	CreatedAt   time.Time
}

type CreateOTPParams struct {
	Channel     Channel
	Destination string
	CodeHash    string
	ExpiresAt   time.Time
	RequestIP   string
	CreatedAt   time.Time
}

type TryCreateOTPParams struct {
	Channel          Channel
	Destination      string
	CodeHash         string
	ExpiresAt        time.Time
	RequestIP        string
	CreatedAt        time.Time
	DestinationSince time.Time
	DestinationLimit int64
	IPSince          time.Time
	IPLimit          int64
}

type ClaimOTPAttemptParams struct {
	ID               uuid.UUID
	Channel          Channel
	Destination      string
	RequestIP        string
	MaxAttempts      int32
	DestinationSince time.Time
	DestinationLimit int64
	IPSince          time.Time
	IPLimit          int64
}

type OTPStore interface {
	TryCreateOTP(ctx context.Context, params TryCreateOTPParams) (OTPRecord, error)
	LatestOTP(ctx context.Context, channel Channel, destination string) (OTPRecord, error)
	ClaimOTPAttempt(ctx context.Context, params ClaimOTPAttemptParams) (int32, error)
	ConsumeOTP(ctx context.Context, id uuid.UUID, at time.Time) error
}

type SendCodeRequest struct {
	Channel     Channel
	Destination string
	RequestIP   string
}

type SentCode struct {
	ID          uuid.UUID
	Channel     Channel
	Destination string
	ExpiresAt   time.Time
}

type VerifyCodeRequest struct {
	Channel     Channel
	Destination string
	Code        string
	RequestIP   string
}

type VerifiedContact struct {
	Channel     Channel
	Destination string
}

type OTPServiceOptions struct {
	Store                   OTPStore
	EmailSender             EmailSender
	SMSSender               SMSSender
	ProductName             string
	TTL                     time.Duration
	MaxAttempts             int32
	SendsPerDestination     int64
	DestinationWindow       time.Duration
	SendsPerIP              int64
	IPWindow                time.Duration
	VerifiesPerDestination  int64
	VerifyDestinationWindow time.Duration
	VerifiesPerIP           int64
	VerifyIPWindow          time.Duration
	HashCost                int
	Clock                   func() time.Time
}

type OTPService struct {
	store                   OTPStore
	email                   EmailSender
	sms                     SMSSender
	productName             string
	ttl                     time.Duration
	maxAttempts             int32
	sendsPerDestination     int64
	destinationWindow       time.Duration
	sendsPerIP              int64
	ipWindow                time.Duration
	verifiesPerDestination  int64
	verifyDestinationWindow time.Duration
	verifiesPerIP           int64
	verifyIPWindow          time.Duration
	hashCost                int
	clock                   func() time.Time
}

func NewOTPService(options OTPServiceOptions) (*OTPService, error) {
	if options.Store == nil {
		return nil, errors.New("auth: an otp store is required")
	}
	service := &OTPService{
		store:                   options.Store,
		email:                   options.EmailSender,
		sms:                     options.SMSSender,
		productName:             firstNonEmpty(options.ProductName, "Afterword"),
		ttl:                     positiveDuration(options.TTL, DefaultOTPTTL),
		maxAttempts:             positiveInt32(options.MaxAttempts, DefaultOTPMaxAttempts),
		sendsPerDestination:     positiveInt64(options.SendsPerDestination, DefaultSendsPerDestination),
		destinationWindow:       positiveDuration(options.DestinationWindow, DefaultDestinationWindow),
		sendsPerIP:              positiveInt64(options.SendsPerIP, DefaultSendsPerIP),
		ipWindow:                positiveDuration(options.IPWindow, DefaultIPWindow),
		verifiesPerDestination:  positiveInt64(options.VerifiesPerDestination, DefaultVerifiesPerDestination),
		verifyDestinationWindow: positiveDuration(options.VerifyDestinationWindow, DefaultVerifyDestinationWindow),
		verifiesPerIP:           positiveInt64(options.VerifiesPerIP, DefaultVerifiesPerIP),
		verifyIPWindow:          positiveDuration(options.VerifyIPWindow, DefaultVerifyIPWindow),
		hashCost:                options.HashCost,
		clock:                   options.Clock,
	}
	if service.hashCost <= 0 {
		service.hashCost = bcrypt.DefaultCost
	}
	if service.clock == nil {
		service.clock = time.Now
	}
	return service, nil
}

func (s *OTPService) Send(ctx context.Context, request SendCodeRequest) (SentCode, error) {
	channel, destination, err := normalizeContact(request.Channel, request.Destination)
	if err != nil {
		return SentCode{}, err
	}
	if err := s.senderFor(channel); err != nil {
		return SentCode{}, err
	}

	now := s.clock().UTC()

	code, err := newNumericCode()
	if err != nil {
		return SentCode{}, err
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(code), s.hashCost)
	if err != nil {
		return SentCode{}, fmt.Errorf("hash verification code: %w", err)
	}

	record, err := s.store.TryCreateOTP(ctx, TryCreateOTPParams{
		Channel:          channel,
		Destination:      destination,
		CodeHash:         string(hashed),
		ExpiresAt:        now.Add(s.ttl),
		RequestIP:        request.RequestIP,
		CreatedAt:        now,
		DestinationSince: now.Add(-s.destinationWindow),
		DestinationLimit: s.sendsPerDestination,
		IPSince:          now.Add(-s.ipWindow),
		IPLimit:          s.sendsPerIP,
	})
	if err != nil {
		return SentCode{}, err
	}

	if err := s.deliver(ctx, channel, destination, code); err != nil {
		return SentCode{}, err
	}

	return SentCode{
		ID:          record.ID,
		Channel:     channel,
		Destination: destination,
		ExpiresAt:   record.ExpiresAt,
	}, nil
}

func (s *OTPService) Verify(ctx context.Context, request VerifyCodeRequest) (VerifiedContact, error) {
	channel, destination, err := normalizeContact(request.Channel, request.Destination)
	if err != nil {
		return VerifiedContact{}, err
	}
	if !codePattern.MatchString(request.Code) {
		return VerifiedContact{}, ErrCodeMismatch
	}

	record, err := s.store.LatestOTP(ctx, channel, destination)
	if err != nil {
		return VerifiedContact{}, err
	}

	now := s.clock().UTC()
	if !now.Before(record.ExpiresAt) {
		return VerifiedContact{}, ErrCodeExpired
	}

	if _, err := s.store.ClaimOTPAttempt(ctx, ClaimOTPAttemptParams{
		ID:               record.ID,
		Channel:          channel,
		Destination:      destination,
		RequestIP:        request.RequestIP,
		MaxAttempts:      s.maxAttempts,
		DestinationSince: now.Add(-s.verifyDestinationWindow),
		DestinationLimit: s.verifiesPerDestination,
		IPSince:          now.Add(-s.verifyIPWindow),
		IPLimit:          s.verifiesPerIP,
	}); err != nil {
		return VerifiedContact{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(record.CodeHash), []byte(request.Code)); err != nil {
		return VerifiedContact{}, ErrCodeMismatch
	}

	if err := s.store.ConsumeOTP(ctx, record.ID, now); err != nil {
		return VerifiedContact{}, err
	}

	return VerifiedContact{Channel: channel, Destination: destination}, nil
}

func (s *OTPService) senderFor(channel Channel) error {
	switch channel {
	case ChannelEmail:
		if s.email == nil {
			return fmt.Errorf("%w: no email sender", ErrNotConfigured)
		}
	case ChannelPhone:
		if s.sms == nil {
			return fmt.Errorf("%w: no sms sender", ErrNotConfigured)
		}
	default:
		return ErrInvalidChannel
	}
	return nil
}

func (s *OTPService) deliver(ctx context.Context, channel Channel, destination, code string) error {
	body := fmt.Sprintf("Your %s sign-in code is %s. It expires in %d minutes.",
		s.productName, code, int(s.ttl/time.Minute))

	switch channel {
	case ChannelEmail:
		message := EmailMessage{
			To:      destination,
			Subject: fmt.Sprintf("%s sign-in code: %s", s.productName, code),
			Text:    body,
		}
		if err := s.email.SendEmail(ctx, message); err != nil {
			return fmt.Errorf("send verification email: %w", err)
		}
	case ChannelPhone:
		if err := s.sms.SendSMS(ctx, destination, body); err != nil {
			return fmt.Errorf("send verification text: %w", err)
		}
	default:
		return ErrInvalidChannel
	}
	return nil
}

func normalizeContact(channel Channel, destination string) (Channel, string, error) {
	if !channel.Valid() {
		return "", "", ErrInvalidChannel
	}
	trimmed := strings.TrimSpace(destination)
	if trimmed == "" || len(trimmed) > maxDestinationSize {
		return "", "", ErrInvalidDestination
	}

	switch channel {
	case ChannelEmail:
		address, err := mail.ParseAddress(trimmed)
		if err != nil {
			return "", "", ErrInvalidDestination
		}
		normalized := strings.ToLower(address.Address)
		if strings.Count(normalized, "@") != 1 || strings.HasPrefix(normalized, "@") || strings.HasSuffix(normalized, "@") {
			return "", "", ErrInvalidDestination
		}
		if len(normalized) > maxDestinationSize {
			return "", "", ErrInvalidDestination
		}
		return channel, normalized, nil
	case ChannelPhone:
		if !phonePattern.MatchString(trimmed) {
			return "", "", ErrInvalidDestination
		}
		return channel, trimmed, nil
	default:
		return "", "", ErrInvalidChannel
	}
}

func NormalizeEmail(value string) (string, error) {
	_, normalized, err := normalizeContact(ChannelEmail, value)
	if err != nil {
		return "", err
	}
	return normalized, nil
}

func NormalizePhone(value string) (string, error) {
	_, normalized, err := normalizeContact(ChannelPhone, value)
	if err != nil {
		return "", err
	}
	return normalized, nil
}

func newNumericCode() (string, error) {
	value, err := rand.Int(rand.Reader, codeCeiling)
	if err != nil {
		return "", fmt.Errorf("generate verification code: %w", err)
	}
	return fmt.Sprintf("%0*d", otpCodeDigits, value.Int64()), nil
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func positiveInt32(value, fallback int32) int32 {
	if value <= 0 {
		return fallback
	}
	return value
}

func positiveInt64(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}
