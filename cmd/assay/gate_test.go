package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/fleet"
)

func TestEvalFailOn(t *testing.T) {
	// default (unsafe) threshold
	assert.NotNil(t, evalFailOn("unsafe", 3, "unsafe"), "unsafe must gate at unsafe threshold")
	assert.Nil(t, evalFailOn("caution", 1, "unsafe"), "caution must NOT gate at unsafe threshold")
	assert.Nil(t, evalFailOn("safe", 0, "unsafe"))

	// caution threshold gates caution AND unsafe
	assert.NotNil(t, evalFailOn("caution", 1, "caution"))
	assert.NotNil(t, evalFailOn("unsafe", 1, "caution"))
	assert.Nil(t, evalFailOn("safe", 0, "caution"))

	// any → gate when any finding survived, regardless of label
	assert.NotNil(t, evalFailOn("safe", 1, "any"))
	assert.Nil(t, evalFailOn("safe", 0, "any"))

	// off → never gate
	assert.Nil(t, evalFailOn("unsafe", 5, "off"))
	assert.Nil(t, evalFailOn("unsafe", 5, "never"))

	// empty / unknown threshold defaults to unsafe
	assert.NotNil(t, evalFailOn("unsafe", 1, ""))

	// exit code is 2 (distinct from 1 = crash)
	ec := evalFailOn("unsafe", 1, "unsafe")
	require.NotNil(t, ec)
	assert.Equal(t, 2, ec.ExitCode())
}

func TestEvalFleetFailOn(t *testing.T) {
	unsafe := &fleet.Report{}
	unsafe.Verdict.Unsafe = 2

	caution := &fleet.Report{}
	caution.Verdict.Caution = 1

	clean := &fleet.Report{}
	clean.Verdict.Safe = 5

	assert.NotNil(t, evalFleetFailOn(unsafe, "unsafe"))
	assert.Nil(t, evalFleetFailOn(caution, "unsafe"), "caution-only fleet must not gate at unsafe")
	assert.NotNil(t, evalFleetFailOn(caution, "caution"))
	assert.Nil(t, evalFleetFailOn(clean, "unsafe"))
	assert.Nil(t, evalFleetFailOn(unsafe, "off"))
	assert.Nil(t, evalFleetFailOn(nil, "unsafe"))

	ec := evalFleetFailOn(unsafe, "unsafe")
	require.NotNil(t, ec)
	assert.Equal(t, 2, ec.ExitCode())
}
