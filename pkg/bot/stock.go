package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// stockHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) stockHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!stock" {
		contractsAvailable, err := b.loadContracts()
		if err != nil {
			b.log.Errorw("error loading ESI contracts", "error", err)

			msg := fmt.Sprintf("Sorry, some error happened: %s", err.Error())
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding with error", "error", err)
				return
			}
			return
		}
		gotDoctrines := doctrinesAvailable(contractsAvailable)
		_, err = b.discord.ChannelMessageSendEmbed(m.ChannelID, stockMessage(gotDoctrines))
		if err != nil {
			b.log.Errorw("error sending message for !want list", "error", err)
			return
		}
		return
	}
}

func stockMessage(doctrines map[string]int) *discordgo.MessageEmbed {
	var (
		names []string // used for sorting by name
		parts []string
	)

	for haveDoctrine := range doctrines {
		names = append(names, haveDoctrine)
	}
	sort.Strings(names)

	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d %s", doctrines[name], name))
	}

	return &discordgo.MessageEmbed{
		Title: "Have on contract",
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/pKEZq6F.png",
		},
		Color:       0x00ff00,
		Description: fmt.Sprintf("```\n%s\n```", strings.Join(parts, "\n")),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
	}
}
