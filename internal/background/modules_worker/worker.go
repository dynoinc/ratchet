package modules_worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/modules"
)

type Worker struct {
	river.WorkerDefaults[background.ModulesWorkerArgs]

	bot     *internal.Bot
	modules []modules.Handler
	tracer  trace.Tracer
}

func New(bot *internal.Bot, modules []modules.Handler) *Worker {
	return &Worker{
		bot:     bot,
		modules: modules,
		tracer:  otel.Tracer("ratchet.modules_worker"),
	}
}

func (w *Worker) Timeout(job *river.Job[background.ModulesWorkerArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.ModulesWorkerArgs]) error {
	if job.Args.ParentTS == "" {
		return w.handleMessage(ctx, job)
	}

	return w.handleThreadMessage(ctx, job)
}

func (w *Worker) handleThreadMessage(ctx context.Context, job *river.Job[background.ModulesWorkerArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			slog.WarnContext(ctx, "thread message not found", "channel_id", job.Args.ChannelID, "slack_ts", job.Args.SlackTS)
			return nil
		}

		return err
	}

	hub := sentry.GetHubFromContext(ctx)
	client := hub.Client()
	scope := hub.Scope()
	scope.SetTag("channel_id", job.Args.ChannelID)
	scope.SetTag("slack_ts", job.Args.SlackTS)
	scope.SetTag("parent_ts", job.Args.ParentTS)

	for _, module := range w.modules {
		threadHandler, ok := module.(modules.ThreadHandler)
		if !ok {
			continue
		}

		if job.Args.IsBackfill {
			if enabled, ok := module.(modules.OnBackfillMessage); !ok || !enabled.EnabledForBackfill() {
				continue
			}
		}

		ctx, innerSpan := w.tracer.Start(ctx, module.Name())
		innerScope := scope.Clone()
		innerScope.SetTag("module", module.Name())

		err := threadHandler.OnThreadMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, job.Args.ParentTS, msg.Attrs)
		if err != nil {
			slog.ErrorContext(ctx, "thread module error", "module", module.Name(), "error", err)
			innerSpan.SetStatus(codes.Error, err.Error())
			client.CaptureException(err, &sentry.EventHint{Context: ctx}, innerScope)
		} else {
			innerSpan.SetStatus(codes.Ok, "ok")
		}

		innerSpan.End()
	}

	return nil
}

func (w *Worker) handleMessage(ctx context.Context, job *river.Job[background.ModulesWorkerArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			slog.WarnContext(ctx, "thread message not found", "channel_id", job.Args.ChannelID, "thread_ts", job.Args.ParentTS)
			return nil
		}

		return err
	}

	hub := sentry.GetHubFromContext(ctx)
	client := hub.Client()
	scope := hub.Scope()
	scope.SetTag("channel_id", job.Args.ChannelID)
	scope.SetTag("slack_ts", job.Args.SlackTS)

	for _, module := range w.modules {
		if job.Args.IsBackfill {
			if enabled, ok := module.(modules.OnBackfillMessage); !ok || !enabled.EnabledForBackfill() {
				continue
			}
		}

		ctx, innerSpan := w.tracer.Start(ctx, module.Name())
		innerScope := scope.Clone()
		innerScope.SetTag("module", module.Name())

		if err := module.OnMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, msg.Attrs); err != nil {
			slog.ErrorContext(ctx, "module error", "module", module.Name(), "error", err)
			innerSpan.SetStatus(codes.Error, err.Error())
			client.CaptureException(err, &sentry.EventHint{Context: ctx}, innerScope)
		} else {
			innerSpan.SetStatus(codes.Ok, "ok")
		}

		innerSpan.End()
	}

	return nil
}
