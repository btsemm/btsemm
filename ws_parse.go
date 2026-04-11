package btsemm

import "encoding/json"

// ParseTrades parses the data from a tradeHistoryApi message.
func (m WSMessage) ParseTrades() ([]WSTrade, error) {
	var out []WSTrade
	return out, json.Unmarshal(m.Data, &out)
}

// ParseSnapshotL1 parses the data from a snapshotL1 message.
func (m WSMessage) ParseSnapshotL1() (*WSSnapshotL1, error) {
	var out WSSnapshotL1
	return &out, json.Unmarshal(m.Data, &out)
}

// ParseOrderbookUpdate parses the data from an orderbook update message.
func (m WSMessage) ParseOrderbookUpdate() (*WSOrderbookUpdate, error) {
	var out WSOrderbookUpdate
	return &out, json.Unmarshal(m.Data, &out)
}
