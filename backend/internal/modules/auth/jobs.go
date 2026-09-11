package auth

import (
	"context"
	"log/slog"

	"bgl/internal/platform/jobs"

	"github.com/riverqueue/river"
)

type SendVerificationEmailArgs struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

func (SendVerificationEmailArgs) Kind() string { return "auth.send_verification_email" }

type SendVerificationEmailWorker struct {
	river.WorkerDefaults[SendVerificationEmailArgs]
	Logger *slog.Logger
	Mailer Mailer // nil is valid — see doc comment above
}

func (w *SendVerificationEmailWorker) Work(ctx context.Context, job *river.Job[SendVerificationEmailArgs]) error {
	if w.Mailer == nil {
		w.Logger.InfoContext(ctx, "auth: would send verification email (no SMTP configured — set SMTP_HOST)",
			"job_id", job.ID,
			"user_id", job.Args.UserID,
			"email", job.Args.Email,
		)
		return nil
	}

	if err := w.Mailer.SendVerificationEmail(ctx, job.Args.Email, job.Args.Username, job.Args.Token); err != nil {
		w.Logger.ErrorContext(ctx, "auth: sending verification email failed",
			"job_id", job.ID,
			"user_id", job.Args.UserID,
			"error", err,
		)
		return err
	}

	w.Logger.InfoContext(ctx, "auth: sent verification email",
		"job_id", job.ID,
		"user_id", job.Args.UserID,
		"email", job.Args.Email,
	)
	return nil
}

func RegisterWorkers(logger *slog.Logger, mailer Mailer) jobs.WorkerRegistrar {
	return func(workers *river.Workers) {
		river.AddWorker(workers, &SendVerificationEmailWorker{Logger: logger, Mailer: mailer})
	}
}
