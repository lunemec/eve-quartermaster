package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/pkg/errors"
)

// reportHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) reportHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!report full" {
		corporationDoctrines, allianceDoctrines, err := b.reportFull()
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
			b.reportFullMessage(corporationDoctrines, allianceDoctrines),
		)
		if err != nil {
			b.log.Errorw("error sending report message", "error", err)
		}
		return
	}

	if m.Content == "!report" || m.Content == "!qm" {
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

func (b *quartermasterBot) reportFull() ([]doctrineReport, []doctrineReport, error) {
	corpContracts, allianceContracts, err := b.loadContracts()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to load contracts")
	}

	gotCorpDoctrines := doctrinesAvailable(corpContracts)
	gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
	wantAllDoctrines, err := b.repository.Read()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error reading wanted doctrines")
	}

	wantCorporationDoctrines := filterDoctrines(wantAllDoctrines, repository.Corporation)
	wantAllianceDoctrines := filterDoctrines(wantAllDoctrines, repository.Alliance)

	return b.fullDoctrines(wantCorporationDoctrines, gotCorpDoctrines),
		b.fullDoctrines(wantAllianceDoctrines, gotAllianceDoctrines),
		nil
}

func (b *quartermasterBot) fullDoctrines(
	wantDoctrines []repository.Doctrine,
	gotDoctrines map[string]int,
) []doctrineReport {
	var doctrines []doctrineReport
	for _, wantDoctrine := range wantDoctrines {
		if wantDoctrine.WantInStock == 0 {
			continue
		}
		var found bool
		for doctrine, haveInStock := range gotDoctrines {
			namesEqual := compareDoctrineNames(wantDoctrine.Name, doctrine)

			if namesEqual {
				found = true
				doctrines = append(doctrines, doctrineReport{
					doctrine:    wantDoctrine,
					haveInStock: haveInStock,
				})
			}
		}
		if !found {
			doctrines = append(doctrines, doctrineReport{
				doctrine:    wantDoctrine,
				haveInStock: 0,
			})
		}
	}
	sort.Slice(doctrines, func(i, j int) bool {
		return doctrines[i].doctrine.Name < doctrines[j].doctrine.Name
	})
	return doctrines
}

func (b *quartermasterBot) reportFullMessage(corporationDoctrines, allianceDoctrines []doctrineReport) *discordgo.MessageEmbed {
	var (
		parts      []string
		msgOK      = ":small_blue_diamond: **%s** - got %d, want %d"
		msgMissing = ":small_orange_diamond: **%s** - got %d, want %d"
	)

	parts = append(parts, ":scroll: ***Alliance contracts***")
	for _, doctrine := range allianceDoctrines {
		msg := msgOK
		if doctrine.haveInStock < doctrine.doctrine.WantInStock {
			msg = msgMissing
		}
		parts = append(parts, fmt.Sprintf(msg,
			doctrine.doctrine.Name,
			doctrine.haveInStock,
			doctrine.doctrine.WantInStock,
		))
	}

	parts = append(parts, "\n :scroll: ***Corporation contracts***")
	for _, doctrine := range corporationDoctrines {
		msg := msgOK
		if doctrine.haveInStock < doctrine.doctrine.WantInStock {
			msg = msgMissing
		}
		parts = append(parts, fmt.Sprintf(msg,
			doctrine.doctrine.Name,
			doctrine.haveInStock,
			doctrine.doctrine.WantInStock,
		))
	}

	var color = 0x00ff00
	return &discordgo.MessageEmbed{
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       color,
		Description: strings.Join(parts, "\n"),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:       "Doctrine ship full report",
	}
}
