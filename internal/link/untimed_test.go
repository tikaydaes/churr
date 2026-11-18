package link_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/link"
)

// The scenarios that, on a slow machine, used to wait on a timer to recover
// (a missed or early answer about a channel's history, a request that came
// before what made it answerable), run with every timer that asks or sends
// again switched off: each passes only if every step is told to whoever
// waits for it the moment it can be.

func untimed(t *testing.T, scenario func(*testing.T)) {
	defer link.SetRetriesUntimed()()
	// And a hub asks a neighbour again for what it put off only when what
	// it waited for has come.
	defer hub.SetPutOffWait(time.Hour)()
	scenario(t)
}

func TestUntimedMoveOntoAHubAcrossACluster(t *testing.T) {
	untimed(t, TestMoveOntoAHubAcrossACluster)
}

func TestUntimedServerAdmittedWhileAway(t *testing.T) {
	untimed(t, TestServerAdmittedWhileAway)
}

func TestUntimedHubChannelReachesOnlyItsServers(t *testing.T) {
	untimed(t, TestHubChannelReachesOnlyItsServers)
}

func TestUntimedHubRestart(t *testing.T) {
	untimed(t, TestHubRestart)
}

func TestUntimedHistoryOutlivesAnInstanceThatLeft(t *testing.T) {
	untimed(t, TestHistoryOutlivesAnInstanceThatLeft)
}

func TestUntimedManagersChangeTheRecord(t *testing.T) {
	untimed(t, TestManagersChangeTheRecord)
}
