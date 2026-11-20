package manage

import (
	"bytes"
	"testing"
)

func TestPlatformEmptyCollectionsRemainEditable(t *testing.T) {
	config, err := decodePlatform([]byte(`{"app":{"schema":4,"nodes":null},"certificates":null}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := encodePlatform(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"nodes":[]`)) || !bytes.Contains(data, []byte(`"certificates":[]`)) {
		t.Fatal("editable collections must serialize as arrays")
	}
}
