package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
)

type migration struct {
	ChannelID string
	MessageID string
	From      string
	To        string
	Created   time.Time
}

// migrate will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) migrate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Force reloading of price from API.
	if strings.HasPrefix(m.Content, "!migrate") {
		paramsStr := strings.TrimPrefix(m.Content, "!migrate")
		paramsStr = strings.TrimSpace(paramsStr)
		params := strings.Split(paramsStr, " ")
		if len(params) != 2 {
			msg := fmt.Sprintf("Bad format, use `!migrate FROM TO`, see `!help` for more info.")
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding to bad !migrate", "error", err)
				return
			}
			return
		}
		b.log.Infow("Responding to !migrate", "channel_id", m.ChannelID, "msg", m.Content, "params", params)

		migrateFrom, migrateTo := params[0], params[1]
		message := b.migrateConfirmMessage(migrateFrom, migrateTo)

		msg, err := b.discord.ChannelMessageSendEmbed(m.ChannelID, message)
		if err != nil {
			b.log.Errorw("error sending message for !migrate", "error", err)

			b.sendError(err, m.ChannelID)
			return
		}

		b.pendingMigrations.Store(msg.ID, migration{
			ChannelID: msg.ChannelID,
			MessageID: msg.ID,
			From:      migrateFrom,
			To:        migrateTo,
			Created:   time.Now().UTC(),
		})
	}
}

func (b *quartermasterBot) migrateConfirmMessage(migrateFrom, migrateTo string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       0xffff00,
		Description: fmt.Sprintf("About to migrate \"%s\" -> \"%s\". Confirm by reacting :white_check_mark:", migrateFrom, migrateTo),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:       "Migrate :question:",
	}
}

func (b *quartermasterBot) migrateReact(s *discordgo.Session, m *discordgo.MessageReactionAdd) {
	if m.Emoji.APIName() != "✅" {
		return
	}

	migrationInterface, ok := b.pendingMigrations.LoadAndDelete(m.MessageID)
	if !ok {
		return
	}
	migration := migrationInterface.(migration)
	if migration.Created.Before(time.Now().UTC().Add(-10 * time.Minute)) {
		ref := discordgo.MessageReference{
			MessageID: migration.MessageID,
			ChannelID: migration.ChannelID,
		}
		_, err := b.discord.ChannelMessageSendReply(migration.ChannelID, "Sorry, the migration request is only valid for 10 minutes.", &ref)
		if err != nil {
			b.log.Errorw("error sending message for !migrate", "error", err)

			b.sendError(err, m.ChannelID)
			return
		}
		return
	}
	b.log.Infow("Applying migration", "migration", migration)

	// This migrates all required doctrine names.
	allDoctrines, err := b.repository.ReadAll()
	if err != nil {
		b.log.Errorw("error loading doctrines", "error", err)

		b.sendError(err, m.ChannelID)
		return
	}
	for i, doctrine := range allDoctrines {
		doctrine.Name = strings.ReplaceAll(doctrine.Name, migration.From, migration.To)
		allDoctrines[i] = doctrine
	}
	err = b.repository.WriteAll(allDoctrines)
	if err != nil {
		b.log.Errorw("error writing doctrines", "error", err)

		b.sendError(err, m.ChannelID)
		return
	}

	// We don't migrate all historical prices because we might want to keep
	// historical records, so we will create new prices from the old prices
	// and keep the highest one (the one visible in full report).
	allPrices, err := b.repository.Prices()
	if err != nil {
		b.log.Errorw("error loading all prices", "error", err)

		b.sendError(err, m.ChannelID)
		return
	}
	priceByDoctrine := make(map[string]uint64)
	for _, doctrinePrice := range allPrices {
		oldPrice := priceByDoctrine[doctrinePrice.DoctrineName]
		if oldPrice < doctrinePrice.Price {
			priceByDoctrine[doctrinePrice.DoctrineName] = doctrinePrice.Price
		}
	}
	var pricedata []repository.PriceData
	for doctrineName, doctrinePrice := range priceByDoctrine {
		doctrineName = strings.ReplaceAll(doctrineName, migration.From, migration.To)

		pricedata = append(pricedata, repository.PriceData{
			DoctrineName: doctrineName,
			Price:        doctrinePrice,
			Timestamp:    time.Now().UTC(),
		})
	}

	err = b.repository.WriteAllPrices(pricedata)
	if err != nil {
		b.log.Errorw("error saving prices data", "error", err)

		b.sendError(err, m.ChannelID)
		return
	}

	err = b.discord.MessageReactionAdd(m.ChannelID, m.MessageID, `👍`)
	if err != nil {
		b.log.Errorw("error reacting with :+1:", "error", err)
		return
	}
}
