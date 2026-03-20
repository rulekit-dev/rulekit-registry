package port

import "context"

type Mailer interface {
	SendOTP(ctx context.Context, toEmail, code string) error
}
