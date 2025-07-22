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

// executeModuleWithTracing executes a module operation with tracing and error handling
func (w *Worker) executeModuleWithTracing(ctx context.Context, module modules.Handler, channelID, slackTS string, fn func(context.Context) error) {
	ctx, span := w.tracer.Start(ctx, module.Name())
	defer span.End()

	hub := sentry.GetHubFromContext(ctx)
	scope := hub.Scope().Clone()
	scope.SetTag("module", module.Name())

	if err := fn(ctx); err != nil {
		slog.ErrorContext(ctx, "module error", "module", module.Name(), "error", err)
		span.SetStatus(codes.Error, err.Error())
		hub.Client().CaptureException(err, &sentry.EventHint{Context: ctx}, scope)
	} else {
		span.SetStatus(codes.Ok, "ok")
	}
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

		w.executeModuleWithTracing(ctx, module, job.Args.ChannelID, job.Args.SlackTS, func(ctx context.Context) error {
			return threadHandler.OnThreadMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, job.Args.ParentTS, msg.Attrs)
		})
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
	scope := hub.Scope()
	scope.SetTag("channel_id", job.Args.ChannelID)
	scope.SetTag("slack_ts", job.Args.SlackTS)

	for _, module := range w.modules {
		if job.Args.IsBackfill {
			if enabled, ok := module.(modules.OnBackfillMessage); !ok || !enabled.EnabledForBackfill() {
				continue
			}
		}

		w.executeModuleWithTracing(ctx, module, job.Args.ChannelID, job.Args.SlackTS, func(ctx context.Context) error {
			return module.OnMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, msg.Attrs)
		})
	}

	return nil
}
