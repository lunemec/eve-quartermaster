package bot

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/lunemec/eve-quartermaster/pkg/token"

	"github.com/antihax/goesi"
	"github.com/antihax/goesi/esi"
	"github.com/antihax/goesi/optional"
	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
)

const discordMaxDescriptionLength = 4096

// Bot what a bot does.
type Bot interface {
	Bot() error
}

type botRepository interface {
	repository.Repository
	repository.PriceHistory
}

type quartermasterBot struct {
	ctx         context.Context
	tokenSource token.Source
	log         logger
	esi         *goesi.APIClient
	discord     *discordgo.Session
	channelID   string

	corporationID int32
	allianceID    int32

	checkInterval  time.Duration
	notifyInterval time.Duration

	repository botRepository

	// mapping of "requireed" doctrine name last notify time
	notified map[string]time.Time

	// ID -> names map
	names *sync.Map

	// Map of migrations to apply by reacting to message.
	pendingMigrations *sync.Map
}

type logger interface {
	Infow(string, ...interface{})
	Errorw(string, ...interface{})
}

// NewQuartermasterBot returns new bot instance.
func NewQuartermasterBot(
	log logger,
	client *http.Client,
	tokenSource token.Source,
	discord *discordgo.Session,
	channelID string,
	corporationID, allianceID int32,
	repository botRepository,
	checkInterval, notifyInterval time.Duration,
) Bot {
	log.Infow("EVE Quartermaster starting",
		"check_interval", checkInterval,
		"notify_interval", notifyInterval,
	)

	esi := goesi.NewAPIClient(client, "EVE Quartermaster (lu.nemec@gmail.com)")
	return &quartermasterBot{
		ctx:               context.WithValue(context.Background(), goesi.ContextOAuth2, tokenSource),
		tokenSource:       tokenSource,
		log:               log,
		esi:               esi,
		discord:           discord,
		channelID:         channelID,
		corporationID:     corporationID,
		allianceID:        allianceID,
		checkInterval:     checkInterval,
		notifyInterval:    notifyInterval,
		repository:        repository,
		notified:          make(map[string]time.Time),
		names:             new(sync.Map),
		pendingMigrations: new(sync.Map),
	}
}

// Bot - you know, do what a bot does.
func (b *quartermasterBot) Bot() error {
	err := b.discord.Open()
	if err != nil {
		return errors.Wrap(err, "unable to connect to discord")
	}
	// Add handler to listen for "!help" messages as help message.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.helpHandler)))
	// Add handler to listen for "!parse excel" messages for bulk insert from excel (or google) sheet.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.parseExcelHandler)))
	// Add handler to listen for "!qm" messages to show missing doctrines on contract.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.reportHandler)))
	// Add handler to listen for "!stock" messages to list currently available doctrines in stock.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.stockHandler)))
	// Add handler to listen for "!require" messages to manage target doctrine numbers to be stocked.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.requireHandler)))
	// Add handler to listen for "!price" messages to record doctrine price history.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.recordPrice)))
	// Add handler to listen for "!leaderboard" messages to show hauling leaderboard.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.leaderboard)))
	// Add handler to listen for "!migrate" messages to migrate doctrines.
	b.discord.AddHandler(IgnoreSelfMessages(IgnorePrivateMessages(b.migrate)))
	b.discord.AddHandler(b.migrateReact)

	return b.runForever()
}

func IgnoreSelfMessages(
	handler func(*discordgo.Session, *discordgo.MessageCreate),
) func(*discordgo.Session, *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by the bot itself.
		if m.Author.ID == s.State.User.ID {
			return
		}
		handler(s, m)
	}
}

func IgnorePrivateMessages(
	handler func(*discordgo.Session, *discordgo.MessageCreate),
) func(*discordgo.Session, *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all private messages (guild_id is not present).
		if m.GuildID == "" {
			return
		}
		handler(s, m)
	}
}

type doctrineReport struct {
	doctrine    repository.Doctrine
	haveInStock int
}

func (b *quartermasterBot) runForever() error {
	for {
		var (
			shouldNotifyDoctrine bool
			allDoctrines         []doctrineReport
			notifyDoctrines      = make(map[string]struct{})
		)

		missingCorpDoctrines, missingAllianceDoctrines, _, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)
			goto SLEEP
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
	SLEEP:
		time.Sleep(b.checkInterval)
	}
}

func (b *quartermasterBot) reportMissing() ([]doctrineReport, []doctrineReport, bool, error) {
	allContracts, err := b.loadContracts()
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "unable to load contracts")
	}

	corporationContracts, allianceContracts := b.filterAndGroupContracts(
		allContracts,
		statusOutstanding,
		typeItemExchange,
		true,
	)

	err = b.trackAndSavePrices(allContracts)
	if err != nil {
		b.log.Errorw("error tracking and saving price history", "error", err)
	}

	gotCorporationDoctrines := doctrinesAvailable(corporationContracts)
	gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
	requireAllDoctrines, err := b.repository.ReadAll()
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "error reading required doctrines")
	}

	requireCorporationDoctrines := filterDoctrines(requireAllDoctrines, repository.Corporation)
	requireAllianceDoctrines := filterDoctrines(requireAllDoctrines, repository.Alliance)

	missingCorporationDoctrines := b.missingDoctrines(requireCorporationDoctrines, gotCorporationDoctrines)
	missingAllianceDoctrines := b.missingDoctrines(requireAllianceDoctrines, gotAllianceDoctrines)

	// If there is no missing contracts and contracts that are required, it means we
	// have everything up on contract and nothing missing.
	allIsOnContract := len(missingCorporationDoctrines)+len(missingAllianceDoctrines) == 0 &&
		len(requireCorporationDoctrines)+len(requireAllianceDoctrines) > 0

	return missingCorporationDoctrines, missingAllianceDoctrines, allIsOnContract, nil
}

func (b *quartermasterBot) trackAndSavePrices(allContracts []esi.GetCorporationsCorporationIdContracts200Ok) error {
	// Check if contract title starts with *, that is used to track price.
	// Example: "* v1 Shield Svipul"
	var contracts []esi.GetCorporationsCorporationIdContracts200Ok
	finishedCorporation, finishedAlliance := b.filterAndGroupContracts(
		allContracts,
		statusFinished,
		typeItemExchange,
		true,
	)
	finishedContractorCorporation, finishedContractorAlliance := b.filterAndGroupContracts(
		allContracts,
		statusFinishedContractor,
		typeItemExchange,
		true,
	)
	finishedIssuerCorporation, finishedIssuerAlliance := b.filterAndGroupContracts(
		allContracts,
		statusFinishedIssuer,
		typeItemExchange,
		true,
	)
	contracts = append(contracts, finishedCorporation...)
	contracts = append(contracts, finishedAlliance...)
	contracts = append(contracts, finishedContractorCorporation...)
	contracts = append(contracts, finishedContractorAlliance...)
	contracts = append(contracts, finishedIssuerCorporation...)
	contracts = append(contracts, finishedIssuerAlliance...)

	for _, contract := range contracts {
		// This is price-tracking contract.
		if strings.HasPrefix(contract.Title, "*") {
			doctrineName := strings.TrimSpace(strings.TrimPrefix(contract.Title, "*"))
			// Check if this is doctrine we track, if not, we don't care about this contract.
			_, err := b.repository.Get(doctrineName)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					continue
				}
				return errors.Wrapf(err, "error loading doctrine: %s", doctrineName)
			}

			contractPrice := uint64(math.Trunc(contract.Price))

			// Deduplication of records is done in the repository.
			err = b.repository.RecordPrice(repository.PriceData{
				DoctrineName: doctrineName,
				Timestamp:    contract.DateIssued,
				ContractID:   contract.ContractId,
				IssuerID:     contract.IssuerId,
				Price:        contractPrice,
			})
			if err != nil {
				return errors.Wrap(err, "error recording price history")
			}
		}
	}

	requireAllDoctrines, err := b.repository.ReadAll()
	if err != nil {
		return errors.Wrap(err, "error reading required doctrines")
	}

	for _, requiredDoctrine := range requireAllDoctrines {
		// Update price to max(price) last 2x doctrine.RequireStock contracts.
		n := requiredDoctrine.RequireStock * 2
		prices, err := b.repository.NPricesForDoctrine(requiredDoctrine.Name, n)
		if err != nil {
			return errors.Wrap(err, "error listing last N prices for doctrine")
		}
		historicalMaxPrice := maxPrice(prices)
		b.log.Infow("Last N prices for", "doctrine", requiredDoctrine.Name, "N", n, "prices", prices, "old", requiredDoctrine.Price, "new", historicalMaxPrice)

		// Update only if the max price is non-zero.
		if historicalMaxPrice.Price != 0 {
			requiredDoctrine.Price.Buy = historicalMaxPrice.Price
			requiredDoctrine.Price.Timestamp = historicalMaxPrice.Timestamp

			err = b.repository.Set(requiredDoctrine.Name, requiredDoctrine)
			if err != nil {
				return errors.Wrap(err, "error saving doctrine")
			}
		}
	}
	return nil
}

func maxPrice(prices []repository.PriceData) repository.PriceData {
	var seen repository.PriceData

	for _, priceDatum := range prices {
		if priceDatum.Price > seen.Price {
			seen = priceDatum
		}
	}

	return seen
}

func filterDoctrines(doctrines []repository.Doctrine, contractedOn repository.ContractedOn) []repository.Doctrine {
	var out []repository.Doctrine
	for _, doctrine := range doctrines {
		if doctrine.ContractedOn == contractedOn {
			out = append(out, doctrine)
		}
	}
	return out
}

func (b *quartermasterBot) missingDoctrines(
	requireDoctrines []repository.Doctrine,
	gotContracts map[string]int,
) []doctrineReport {
	var (
		report []doctrineReport
	)

	doctrinesDiff := diffDoctrines(requireDoctrines, gotContracts)
	for _, doctrineReport := range doctrinesDiff {
		if doctrineReport.haveInStock < doctrineReport.doctrine.RequireStock {
			report = append(report, doctrineReport)
		}
	}

	sort.Slice(report, func(i, j int) bool {
		return report[i].doctrine.Name < report[j].doctrine.Name
	})
	return report
}

func diffDoctrines(
	requireDoctrines []repository.Doctrine,
	gotContracts map[string]int,
) map[string]doctrineReport {
	var (
		missing = make(map[string]doctrineReport)
	)

	// Iterate over contracts, and decrement from missing map for
	// each found doctrine ship.
	for _, requireDoctrine := range requireDoctrines {
		found := false
		for contractName, haveInStock := range gotContracts {
			namesEqual := compareDoctrineNames(requireDoctrine.Name, contractName)
			if namesEqual {
				found = true

				report := missing[requireDoctrine.Name]
				report.doctrine = requireDoctrine
				report.haveInStock += haveInStock
				missing[requireDoctrine.Name] = report
			}
		}
		if !found {
			missing[requireDoctrine.Name] = doctrineReport{
				doctrine:    requireDoctrine,
				haveInStock: 0,
			}
		}
	}

	return missing
}

func filterNotifyDoctrines(notifyDoctrines map[string]struct{}, doctrines []doctrineReport) []doctrineReport {
	var out []doctrineReport
	for _, doctrine := range doctrines {
		_, ok := notifyDoctrines[doctrine.doctrine.Name]
		if ok {
			out = append(out, doctrine)
		}
	}
	return out
}

// loadContracts returns contracts from EVE ESI which are assigned to specified
// assigneeID.
func (b *quartermasterBot) loadContracts() (
	[]esi.GetCorporationsCorporationIdContracts200Ok,
	error,
) {
	var allContracts []esi.GetCorporationsCorporationIdContracts200Ok

	contractsPage, resp, err := b.esi.ESI.ContractsApi.GetCorporationsCorporationIdContracts(b.ctx, b.corporationID, nil)
	if err != nil {
		return nil, errors.Wrap(err, "error calling ESI API")
	}
	allContracts = append(allContracts, contractsPage...)

	pages, err := strconv.Atoi(resp.Header.Get("X-Pages"))
	if err != nil {
		return nil, errors.Wrap(err, "error converting X-Pages to integer")
	}
	// Fetch additional pages if any (starting page above is 1).
	for i := 2; i <= pages; i++ {
		contractsPage, _, err := b.esi.ESI.ContractsApi.GetCorporationsCorporationIdContracts(
			b.ctx,
			b.corporationID,
			&esi.GetCorporationsCorporationIdContractsOpts{
				Page: optional.NewInt32(int32(i)),
			},
		)
		if err != nil {
			return nil, errors.Wrap(err, "error calling ESI API")
		}
		allContracts = append(allContracts, contractsPage...)
	}

	return allContracts, nil
}

type contractStatus string
type contractType string

const (
	// Contract Statuses.
	statusOutstanding        contractStatus = "outstanding"         // nolint
	statusInProgress         contractStatus = "in_progress"         // nolint
	statusFinishedIssuer     contractStatus = "finished_issuer"     // nolint
	statusFinishedContractor contractStatus = "finished_contractor" // nolint
	statusFinished           contractStatus = "finished"            // nolint
	statusCancelled          contractStatus = "cancelled"           // nolint
	statusRejected           contractStatus = "rejected"            // nolint
	statusFailed             contractStatus = "failed"              // nolint
	statusDeleted            contractStatus = "deleted"             // nolint
	statusReversed           contractStatus = "reversed"            // nolint

	// Contract Types.
	typeUnknown      contractType = "unknown"       // nolint
	typeItemExchange contractType = "item_exchange" // nolint
	typeAuction      contractType = "auction"       // nolint
	typeCourier      contractType = "courier"       // nolint
	typeLoadn        contractType = "loan"          // nolint
)

func (b *quartermasterBot) filterAndGroupContracts(
	contracts []esi.GetCorporationsCorporationIdContracts200Ok,
	status contractStatus,
	contractType contractType,
	skipExpired bool,
) (
	[]esi.GetCorporationsCorporationIdContracts200Ok,
	[]esi.GetCorporationsCorporationIdContracts200Ok,
) {
	var (
		corpContracts     []esi.GetCorporationsCorporationIdContracts200Ok
		allianceContracts []esi.GetCorporationsCorporationIdContracts200Ok
	)
	for _, contract := range contracts {
		if contract.Type_ != string(contractType) {
			continue
		}
		// Skip contract that are different status from what we require.
		if contract.Status != string(status) {
			continue
		}
		// Skip expired contracts.
		if skipExpired && contract.DateExpired.Before(time.Now()) {
			continue
		}
		if contract.AssigneeId == b.corporationID {
			corpContracts = append(corpContracts, contract)
		}
		if contract.AssigneeId == b.allianceID {
			allianceContracts = append(allianceContracts, contract)
		}
	}

	return corpContracts, allianceContracts
}

type alertContract struct {
	Contract esi.GetCorporationsCorporationIdContracts200Ok
	Reason   string
}

func (b *quartermasterBot) filterAlertContracts(
	requiredDoctrines []repository.Doctrine,
	contracts []esi.GetCorporationsCorporationIdContracts200Ok,
) []alertContract {
	var (
		alertContracts []alertContract
	)
	for _, requiredDoctrine := range requiredDoctrines {
		for _, contract := range contracts {
			if contract.AssigneeId != b.corporationID && contract.AssigneeId != b.allianceID {
				continue
			}
			// Ignore price-tracking contracts in alerts.
			if strings.HasPrefix(contract.Title, "*") {
				continue
			}
			// Skip contracts that are not required doctrines.
			if !compareDoctrineNames(requiredDoctrine.Name, contract.Title) {
				continue
			}

			switch contract.Status {
			case string(statusCancelled), string(statusDeleted), string(statusFinished), string(statusFinishedContractor), string(statusFinishedIssuer):
				continue
			}
			// Alert if we bought this doctrine for more than we are selling it.
			if requiredDoctrine.Price.Buy > uint64(contract.Price) {
				alertContracts = append(alertContracts, alertContract{
					Contract: contract,
					Reason:   "Price",
				})
			}
			// Alert expired contract.
			if contract.DateExpired.Before(time.Now()) {
				alertContracts = append(alertContracts, alertContract{
					Contract: contract,
					Reason:   "Expired",
				})
			}
			// Alert on wrong type of contract.
			if contract.Type_ != string(typeItemExchange) {
				alertContracts = append(alertContracts, alertContract{
					Contract: contract,
					Reason:   "Wrong contract type",
				})
			}
		}
	}

	return alertContracts
}

func (b *quartermasterBot) idToName(id int32) string {
	nameInterface, ok := b.names.Load(id)
	if !ok {
		names, resp, err := b.esi.ESI.UniverseApi.PostUniverseNames(b.ctx, []int32{id}, nil)
		if err != nil {
			// Do not cache on error, so it will retry next time.
			// Return back ID.
			body, _ := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			b.log.Errorw("Error translating ID to name", "error", err, "body", body)
			return fmt.Sprint(id)
		}
		// If we do not have names, then return the ID back.
		if len(names) != 1 {
			b.log.Errorw("Error translating ID to name, returned no names", "len", len(names))
			return fmt.Sprint(id)
		}
		name := names[0].Name
		b.names.Store(id, name)
		return name
	}
	return nameInterface.(string)
}

// doctrinesAvailable returns map of doctrine name -> count of contracts
func doctrinesAvailable(contracts []esi.GetCorporationsCorporationIdContracts200Ok) map[string]int {
	var out = make(map[string]int)
	for _, contract := range contracts {
		// Skip price-tracking contracts starting with "*".
		if strings.HasPrefix(contract.Title, "*") {
			continue
		}
		var isInAvailable bool
		for doctrineAvailable := range out {
			namesEqual := compareDoctrineNames(doctrineAvailable, contract.Title)
			if namesEqual {
				isInAvailable = true
				out[doctrineAvailable]++
			}
		}
		if !isInAvailable {
			out[contract.Title]++
		}
	}

	return out
}

func compareDoctrineNames(require, have string) bool {
	requireParts := strings.Split(require, " ")
	haveParts := strings.Split(have, " ")

	var requirePartsEqual int
	for _, requirePart := range requireParts {
		requirePartLower := strings.ToLower(requirePart)

		for _, havePart := range haveParts {
			havePartLower := strings.ToLower(havePart)
			if requirePartLower == havePartLower {
				requirePartsEqual++
			}
		}
	}

	// Simple check went OK, all requireParts are found in haveParts.
	if requirePartsEqual >= len(requireParts) {
		return true
	}
	// Otherwise try similarity check.
	// This matches more complicated names containing () and so on.
	metric := metrics.NewJaccard()
	metric.CaseSensitive = false
	similarity := strutil.Similarity(require, have, metric)
	return similarity >= 0.8
}

func (b *quartermasterBot) notifyMessage(
	missingCorporationDoctrines, missingAllianceDoctrines []doctrineReport,
) []*discordgo.MessageEmbed {
	var partsAlliance, partsCorporation []string

	// Add "Alliance" block only if there is something to show there.
	if len(missingAllianceDoctrines) != 0 {
		for _, missingDoctrine := range missingAllianceDoctrines {
			partsAlliance = append(partsAlliance, fmt.Sprintf("**%s** is low in stock, have %d but require %d",
				missingDoctrine.doctrine.Name,
				missingDoctrine.haveInStock,
				missingDoctrine.doctrine.RequireStock,
			))
		}
	}

	// Add "Corporation" block only if there is something to show there.
	if len(missingCorporationDoctrines) != 0 {
		for _, missingDoctrine := range missingCorporationDoctrines {
			partsCorporation = append(partsCorporation, fmt.Sprintf("**%s** is low in stock, have %d but require %d",
				missingDoctrine.doctrine.Name,
				missingDoctrine.haveInStock,
				missingDoctrine.doctrine.RequireStock,
			))
		}
	}

	var (
		messages []*discordgo.MessageEmbed
		color    = 0xff0000
	)
	if len(partsAlliance) > 0 {
		reportMessages := splitMessageParts(partsAlliance, discordMaxDescriptionLength)
		for _, reportMessage := range reportMessages {
			messages = append(messages, &discordgo.MessageEmbed{
				Thumbnail: &discordgo.MessageEmbedThumbnail{
					URL: "https://i.imgur.com/ZwUn8DI.jpg",
				},
				Color:       color,
				Description: reportMessage,
				Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
				Title:       "Doctrine ship contracts low [Alliance]",
			},
			)
		}
	}
	if len(partsCorporation) > 0 {
		reportMessages := splitMessageParts(partsCorporation, discordMaxDescriptionLength)
		for _, reportMessage := range reportMessages {
			messages = append(messages, &discordgo.MessageEmbed{
				Thumbnail: &discordgo.MessageEmbedThumbnail{
					URL: "https://i.imgur.com/ZwUn8DI.jpg",
				},
				Color:       color,
				Description: reportMessage,
				Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
				Title:       "Doctrine ship contracts low [Corporation]",
			},
			)
		}
	}

	return messages
}

// shouldNotify checks if given doctrine should be notified
// right now.
func (b *quartermasterBot) shouldNotify(doctrineName string) bool {
	return !b.wasNotified(doctrineName)
}

// setWasNotified stores information that doctrine was already
// notified at time.Now()
func (b *quartermasterBot) setWasNotified(doctrineName string) {
	b.notified[doctrineName] = time.Now()
}

// wasNotified checks if this doctrine was notified within
// b.notifyInterval.
func (b *quartermasterBot) wasNotified(doctrineName string) bool {
	notifyTime, ok := b.notified[doctrineName]
	if !ok {
		return false
	}
	if time.Since(notifyTime) > b.notifyInterval {
		return false
	}
	return true
}

func (b *quartermasterBot) sendError(errIn error, channelID string) {
	msg := fmt.Sprintf("Sorry, some error happened: %s", errIn.Error())
	_, err := b.discord.ChannelMessageSend(channelID, msg)
	if err != nil {
		b.log.Errorw("error responding with error", "error", err, "original_error", errIn)
	}
}

func (b *quartermasterBot) sendAllOnContractMessage(m *discordgo.MessageCreate) {
	color := 0x00ff00
	msg := &discordgo.MessageEmbed{
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color: color,
		Image: &discordgo.MessageEmbedImage{
			URL: "https://i.imgur.com/rYbXjfI.gif",
		},
		Timestamp: time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:     "Doctrine ship stock :ok_hand:",
	}
	_, err := b.discord.ChannelMessageSendEmbed(
		m.ChannelID,
		msg,
	)
	if err != nil {
		b.log.Errorw("error sending all on contract message", "error", err)
	}
}

func (b *quartermasterBot) sendNoDoctrinesAddedMessage(m *discordgo.MessageCreate) {
	msg := "Nothing added yet, use `!require` command to add doctrines, or check `!help` for more information."
	_, err := b.discord.ChannelMessageSend(
		m.ChannelID,
		msg,
	)
	if err != nil {
		b.log.Errorw("error sending no doctrines added message", "error", err)
	}
}
