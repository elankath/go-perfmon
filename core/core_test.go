package core

import (
	"context"
	"testing"
)

func TestLaunchIostat(t *testing.T) {
	reader, err := launchIostat(context.Background())
	if err != nil {
		t.Error(err)
		return
	}

	//<-time.After(12 * time.Second)

	var metrics []IOMetric
	err = parseIostatOutput(reader, metrics)
	if err != nil {
		t.Error(err)
		return
	}

}

func TestParseIostatLine(t *testing.T) {
	kbt, tps, mbs, err := parseIostatLine("57.03  550 30.60")
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("KBt=%f, tps=%d, mbs=%f", kbt, tps, mbs)
}
