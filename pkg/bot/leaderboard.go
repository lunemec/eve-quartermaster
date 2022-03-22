package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dustin/go-humanize"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
)

// leaderboard will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) leaderboard(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Force reloading of price from API.
	if strings.HasPrefix(m.Content, "!leaderboard") {
		b.log.Infow("Responding to !leaderboard", "channel_id", m.ChannelID)

		now := time.Now()
		currentYear, currentMonth, _ := now.Date()

		firstOfMonth := time.Date(currentYear, currentMonth, 1, 0, 0, 0, 0, time.UTC)
		lastOfMonth := firstOfMonth.AddDate(0, 1, -1)

		priceData, err := b.repository.SeekPrices(firstOfMonth, lastOfMonth)
		if err != nil {
			b.log.Errorw("error seeking price history", "error", err, "start_date", firstOfMonth, "end_date", lastOfMonth)

			b.sendError(err, m)
			return
		}

		message, err := b.leaderboardMessage(priceData)
		if err != nil {
			b.log.Errorw("error seeking price history", "error", err, "start_date", firstOfMonth, "end_date", lastOfMonth)

			b.sendError(err, m)
			return
		}

		_, err = b.discord.ChannelMessageSendEmbed(m.ChannelID, message)
		if err != nil {
			b.log.Errorw("error sending message for !leaderboard", "error", err)

			b.sendError(err, m)
			return
		}
	}
}

type haulingStats struct {
	Contracts  int
	TotalPrice uint64
	IssuerID   int32
}

func (b *quartermasterBot) leaderboardMessage(priceData []repository.PriceData) (*discordgo.MessageEmbed, error) {
	statsPerIssuer := make(map[int32]haulingStats)
	for _, priceDatum := range priceData {
		stats := statsPerIssuer[priceDatum.IssuerID]
		stats.IssuerID = priceDatum.IssuerID
		stats.Contracts++
		stats.TotalPrice += priceDatum.Price
		statsPerIssuer[priceDatum.IssuerID] = stats
	}
	// Now collect into slice and order by number of contracts.
	var stats []haulingStats
	for _, stat := range statsPerIssuer {
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Contracts < stats[j].Contracts
	})
	// We only want top 10 on the leaderboard.
	// TODO

	var msgParts []string
	for i, stat := range stats {
		position := i + 1
		extraIcon := ""
		if position == 1 {
			extraIcon = ":tada: "
		}

		msg := fmt.Sprintf("%s**%s** `%s` with **%d contracts** worth **%d M ISK**",
			extraIcon,
			humanize.Ordinal(position),
			b.idToName(stat.IssuerID),
			stat.Contracts,
			stat.TotalPrice/1000000,
		)
		msgParts = append(msgParts, msg)
	}

	return &discordgo.MessageEmbed{
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       0x00ff00,
		Description: strings.Join(msgParts, "\n"),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:       ":crown: Leaderboard",
	}, nil
}
