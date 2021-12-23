package doctrineContractsBot

import (
	"testing"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/stretchr/testify/assert"
)

func TestSimilarity(t *testing.T) {
	//swg := metrics.NewSorensenDice()
	swg := metrics.NewJaccard()
	swg.CaseSensitive = false
	similarity := strutil.Similarity("v11 Whaling Kirin", "v11 whaling kirin", swg)
	assert.Equal(t, float64(1), similarity)

	similarity = strutil.Similarity("v11 Bomber DPS Manticore", "v11 DPS Bomber Manticore", swg)
	assert.Equal(t, float64(1), similarity)

	similarity = strutil.Similarity("v11 Heavy DPS 3 Gyro", "v11 Heavy DPS (3 Gyro)", swg)
	assert.Equal(t, float64(0.8181818181818182), similarity)

	similarity = strutil.Similarity("v11 Heavy Legion", "v11 Heavy Leshak", swg)
	assert.Equal(t, float64(0.5789473684210527), similarity)

	similarity = strutil.Similarity("v11 Whaling Kirin", "v11 Whaling Kiki", swg)
	assert.Equal(t, float64(0.7222222222222222), similarity)

	similarity = strutil.Similarity("v11 Whaling Kirin", "v11 whaling kirin PITH B", swg)
	assert.Equal(t, float64(1), similarity)
}

func TestCompareDoctrineNames(t *testing.T) {
	similarity := compareDoctrineNames("v11 Whaling Kirin", "v11 whaling kirin")
	assert.Equal(t, true, similarity)

	similarity = compareDoctrineNames("v11 Bomber DPS Manticore", "v11 DPS Bomber Manticore")
	assert.Equal(t, true, similarity)

	similarity = compareDoctrineNames("v11 Heavy DPS 3 Gyro", "v11 Heavy DPS (3 Gyro)")
	assert.Equal(t, true, similarity)

	similarity = compareDoctrineNames("v11 Heavy Legion", "v11 Heavy Leshak")
	assert.Equal(t, false, similarity)

	similarity = compareDoctrineNames("v11 Whaling Kirin", "v11 Whaling Kiki")
	assert.Equal(t, false, similarity)

	similarity = compareDoctrineNames("v11 Whaling Kirin", "v11 whaling kirin PITH B")
	assert.Equal(t, true, similarity)
}
