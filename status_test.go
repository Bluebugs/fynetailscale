package fynetailscale_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"fyne.io/fyne/v2/data/binding"
	"github.com/fynelabs/fynetailscale"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_StatusBinding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lc, _, cancel, err := SetupTailscalePreAuthClient(ctx)
	require.NoError(t, err)
	defer cancel()
	assert.NotNil(t, lc)

	b := fynetailscale.NewStatusBinding(ctx, lc)
	assert.NotNil(t, b)
	b.AddListener(binding.NewDataListener(func() {
		msg, err := b.Get()
		assert.NoError(t, err)
		assert.NotEmpty(t, msg)
		fmt.Println("message:", msg)
	}))

	time.Sleep(time.Minute)
}
