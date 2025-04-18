package modules_worker

import (
	"context"
	"log/slog"

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

func (w *Worker) Work(ctx context.Context, job *river.Job[background.ModulesWorkerArgs]) error {
	if job.Args.ThreadTS == "" {
		return w.handleMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	}

	return w.handleThreadMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, job.Args.ThreadTS)
}

func (w *Worker) handleThreadMessage(ctx context.Context, channelID string, slackTS string, threadTS string) error {
	msg, err := w.bot.GetMessage(ctx, channelID, threadTS)
	if err != nil {
		return err
	}

	for _, module := range w.modules {
		threadHandler, ok := module.(modules.ThreadHandler)
		if !ok {
			continue
		}

		if err := threadHandler.OnThreadMessage(ctx, channelID, slackTS, threadTS, msg.Attrs); err != nil {
			slog.Info("thread module error", "module", module.Name(), "error", err)
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "module",
					Message:  module.Name(),
					Level:    sentry.LevelInfo,
				}, 100)
				sentry.CaptureException(err)
			})
		}
	}

	return nil
}

func (w *Worker) handleMessage(ctx context.Context, channelID string, slackTS string) error {
	msg, err := w.bot.GetMessage(ctx, channelID, slackTS)
	if err != nil {
		return err
	}

	for _, module := range w.modules {
		if err := module.OnMessage(ctx, channelID, slackTS, msg.Attrs); err != nil {
			slog.Info("module error", "module", module.Name(), "error", err)
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "module",
					Message:  module.Name(),
					Level:    sentry.LevelInfo,
				}, 100)
				sentry.CaptureException(err)
			})
		}
	}

	return nil
}
