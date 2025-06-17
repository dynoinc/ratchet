package modules_worker

import (
	"context"
	"errors"
	"fmt"
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

	bot          *internal.Bot
	agentModules []modules.Handler
	modules      []modules.Handler
}

func New(bot *internal.Bot, agentModules []modules.Handler, modules []modules.Handler) *Worker {
	return &Worker{
		bot:          bot,
		agentModules: agentModules,
		modules:      modules,
	}
}

func (w *Worker) Timeout(job *river.Job[background.ModulesWorkerArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.ModulesWorkerArgs]) error {
	channel, err := w.bot.GetChannel(ctx, job.Args.ChannelID)
	if err != nil {
		return fmt.Errorf("getting channel %s: %w", job.Args.ChannelID, err)
	}

	modules := w.modules
	if channel.Attrs.AgentModeEnabled {
		modules = w.agentModules
	}

	if job.Args.ParentTS == "" {
		return w.handleMessage(ctx, job, modules)
	}

	return w.handleThreadMessage(ctx, job, modules)
}

func (w *Worker) handleThreadMessage(ctx context.Context, job *river.Job[background.ModulesWorkerArgs], moduleHandlers []modules.Handler) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			slog.WarnContext(ctx, "thread message not found", "channel_id", job.Args.ChannelID, "slack_ts", job.Args.SlackTS)
			return nil
		}

		return err
	}

	for _, module := range moduleHandlers {
		threadHandler, ok := module.(modules.ThreadHandler)
		if !ok {
			continue
		}

		if job.Args.IsBackfill {
			if enabled, ok := module.(modules.OnBackfillMessage); !ok || !enabled.EnabledForBackfill() {
				continue
			}
		}

		if err := threadHandler.OnThreadMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, job.Args.ParentTS, msg.Attrs); err != nil {
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

func (w *Worker) handleMessage(ctx context.Context, job *river.Job[background.ModulesWorkerArgs], moduleHandlers []modules.Handler) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			slog.WarnContext(ctx, "thread message not found", "channel_id", job.Args.ChannelID, "thread_ts", job.Args.ParentTS)
			return nil
		}

		return err
	}

	for _, module := range moduleHandlers {
		if job.Args.IsBackfill {
			if enabled, ok := module.(modules.OnBackfillMessage); !ok || !enabled.EnabledForBackfill() {
				continue
			}
		}

		if err := module.OnMessage(ctx, job.Args.ChannelID, job.Args.SlackTS, msg.Attrs); err != nil {
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
