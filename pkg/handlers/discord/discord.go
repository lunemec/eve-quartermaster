package discord

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type discordHandler struct {
	ctx     context.Context
	log     *zap.Logger
	discord *discordgo.Session
}

func NewDiscordHandler(ctx context.Context, log *zap.Logger, discord *discordgo.Session) *discordHandler {
	return &discordHandler{
		ctx:     ctx,
		log:     log,
		discord: discord,
	}
}

func (h *discordHandler) Start() {
	// Add handler to listen for "!help" messages as help message.
	h.discord.AddHandler(h.helpHandler)
	// Add handler to listen for "!parse excel" messages for bulk insert from excel (or google) sheet.
	h.discord.AddHandler(h.parseExcelHandler)
	// Add handler to listen for "!qm" messages to show missing doctrines on contract.
	h.discord.AddHandler(h.reportHandler)
	// Add handler to listen for "!stock" messages to list currently available doctrines in stock.
	h.discord.AddHandler(h.stockHandler)
	// Add handler to listen for "!require" messages to manage target doctrine numbers to be stocked.
	h.discord.AddHandler(h.requireHandler)

	<-h.ctx.Done()
}
