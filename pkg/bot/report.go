package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// reportHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) reportHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!qm" {
		missingCorporationDoctrines, missingAllianceDoctrines, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)

			msg := fmt.Sprintf("Sorry, some error happened: %s", err.Error())
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding with error", "error", err)
				return
			}
			return
		}
		_, err = b.discord.ChannelMessageSendEmbed(
			m.ChannelID,
			b.notifyMessage(missingCorporationDoctrines, missingAllianceDoctrines),
		)
		if err != nil {
			b.log.Errorw("error sending report message", "error", err)
		}
		return
	}
}
