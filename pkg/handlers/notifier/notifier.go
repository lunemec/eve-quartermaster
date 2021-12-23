package notifier

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type notifierHandler struct {
	ctx                      context.Context
	log                      *zap.Logger
	checkInterval            time.Duration
	doctrineContractsService doctrineContractsService
}

type doctrineContractsService interface {
}

func NewNotifierHandler(
	ctx context.Context,
	log *zap.Logger,
	checkInterval, notifyInterval time.Duration,
	doctrineContractsService doctrineContractsService,
) *notifierHandler {
	notifier := notifierHandler{
		ctx:                      ctx,
		log:                      log,
		checkInterval:            checkInterval,
		doctrineContractsService: doctrineContractsService,
	}
	return &notifier
}

func (n *notifierHandler) Start() {
	ticker := time.NewTicker(n.checkInterval)
	for {
		select {
		case <-ticker.C:
			err := n.tick()
			if err != nil {
				n.log.Error("notifier error", zap.Error(err))
			}
		case <-n.ctx.Done():
			return
		}
	}
}

// tick is called every ticker interval.
func (n *notifierHandler) tick() error {
	var (
		shouldNotifyDoctrine bool
		allDoctrines         []doctrineReport
		notifyDoctrines      = make(map[string]struct{})
	)

	missingCorpDoctrines, missingAllianceDoctrines, err := b.reportMissing()
	if err != nil {
		b.log.Errorw("Error checking for missing doctrines",
			"error", err,
		)
		return errors.Wrap(err, "")
	}

	allDoctrines = append(missingCorpDoctrines, missingAllianceDoctrines...)

	// If just one of the missing doctrines should be notified about, notify about all.
	for _, missingDoctrine := range allDoctrines {
		shouldNotifyDoctrine = b.shouldNotify(missingDoctrine.doctrine.Name)
		if shouldNotifyDoctrine {
			notifyDoctrines[missingDoctrine.doctrine.Name] = struct{}{}
		}
	}

	if len(notifyDoctrines) != 0 {
		messages := b.notifyMessage(
			filterNotifyDoctrines(notifyDoctrines, missingCorpDoctrines),
			filterNotifyDoctrines(notifyDoctrines, missingAllianceDoctrines),
		)
		if len(messages) == 0 {
			b.log.Infow("No doctrines added yet, sleeping.")
			goto SLEEP
		}
		for _, message := range messages {
			_, err = b.discord.ChannelMessageSendEmbed(
				b.channelID,
				message,
			)
			switch {
			case err != nil:
				b.log.Errorw("Error sending discord message",
					"error", err,
				)
				// In case of error, we fall through to the time.Sleep
				// block. We also do not set the structure as notified
				// and it get picked up on next iteration.
				goto SLEEP
			case err == nil:
				for notifiedDoctrine := range notifyDoctrines {
					b.setWasNotified(notifiedDoctrine)
				}
			}
		}
	}
}
