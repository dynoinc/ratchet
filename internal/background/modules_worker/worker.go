package modules_worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/modules"
)

type Worker struct {
	river.WorkerDefaults[background.ModulesWorkerArgs]

	bot     *internal.Bot
	modules []modules.Handler
}

func New(bot *internal.Bot, modules []modules.Handler) *Worker {
	return &Worker{
		bot:     bot,
		modules: modules,
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

		span := sentry.StartSpan(ctx, module.Name())

		err := threadHandler.OnThreadMessage(span.Context(), job.Args.ChannelID, job.Args.SlackTS, job.Args.ParentTS, msg.Attrs)
		if err != nil {
			slog.Info("thread module error", "module", module.Name(), "error", err)
			span.Status = sentry.SpanStatusInternalError
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("module", module.Name())
				scope.SetTag("channel_id", job.Args.ChannelID)
				scope.SetTag("slack_ts", job.Args.SlackTS)
				scope.SetTag("parent_ts", job.Args.ParentTS)
				scope.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "module",
					Message:  module.Name(),
					Level:    sentry.LevelInfo,
				}, 100)
				sentry.CaptureException(err)
			})
		} else {
			span.Status = sentry.SpanStatusOK
		}

		span.Finish()
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

	for _, module := range w.modules {
		if job.Args.IsBackfill {
			if enabled, ok := module.(modules.OnBackfillMessage); !ok || !enabled.EnabledForBackfill() {
				continue
			}
		}

		span := sentry.StartSpan(ctx, module.Name())

		if err := module.OnMessage(span.Context(), job.Args.ChannelID, job.Args.SlackTS, msg.Attrs); err != nil {
			slog.Info("module error", "module", module.Name(), "error", err)
			span.Status = sentry.SpanStatusInternalError
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("module", module.Name())
				scope.SetTag("channel_id", job.Args.ChannelID)
				scope.SetTag("slack_ts", job.Args.SlackTS)
				scope.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "module",
					Message:  module.Name(),
					Level:    sentry.LevelInfo,
				}, 100)
				sentry.CaptureException(err)
			})
		} else {
			span.Status = sentry.SpanStatusOK
		}

		span.Finish()
	}

	return nil
}
