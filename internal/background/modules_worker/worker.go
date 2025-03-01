package modules_worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/modules"
	"github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"
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
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		return err
	}

	for _, module := range w.modules {
		if err := module.Handle(ctx, job.Args.ChannelID, job.Args.SlackTS, msg.Attrs); err != nil {
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

// Longer timeouts for modules worker because it has to do a lot of things
func (w *Worker) Timeout(job *river.Job[background.ModulesWorkerArgs]) time.Duration {
	return 5 * time.Minute
}
