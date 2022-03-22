package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

var priceSetRegex = regexp.MustCompile(`^(?P<number>[0-9]+)\s(?P<name>.*)$`)

// recordPrice will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) recordPrice(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Force reloading of price from API.
	if strings.HasPrefix(m.Content, "!price fetch") {
		b.log.Infow("Responding to !price fetch", "channel_id", m.ChannelID)

		_, _, _, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("error loading contracts", "error", err)
			return
		}

		err = b.discord.MessageReactionAdd(m.ChannelID, m.ID, `👍`)
		if err != nil {
			b.log.Errorw("error reacting with :+1:", "error", err)
			return
		}
		return
	}

	if strings.HasPrefix(m.Content, "!price set") {
		b.log.Infow("Responding to !price set", "channel_id", m.ChannelID)
		// Format is: "!price set NN Doctrine name", example: "!price set 45000000 Shield Drake"
		commandContent := strings.TrimPrefix(m.Content, "!price set ")
		matches := priceSetRegex.FindAllStringSubmatch(commandContent, -1)

		if len(matches) == 0 || (len(matches) != 0 && len(matches[0]) != 3) {
			// Send back "unrecognised - format is ..."
			msg := fmt.Sprintf("unrecognised !price set `%s`, the format is `!price set NN Some doctrine`", commandContent)
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding to unknown !price set", "error", err)
				return
			}
			return
		}

		doctrineName := matches[0][2]
		doctrinePrice, err := strconv.Atoi(matches[0][1])
		if err != nil {
			b.sendError(err, m)
			return
		}

		doctrine, err := b.repository.Get(doctrineName)
		if err != nil {
			b.log.Errorw("error loading doctrine data", "error", err, "doctrine_name", doctrineName)

			b.sendError(err, m)
			return
		}
		doctrine.Price.Buy = uint64(doctrinePrice)
		doctrine.Price.Timestamp = time.Now().UTC()
		err = b.repository.Set(doctrineName, doctrine)
		if err != nil {
			b.log.Errorw("error saving doctrine data", "error", err, "doctrine_name", doctrineName)

			b.sendError(err, m)
			return
		}
		err = b.discord.MessageReactionAdd(m.ChannelID, m.ID, `👍`)
		if err != nil {
			b.log.Errorw("error reacting with :+1:", "error", err)
			return
		}
		return
	}
}
