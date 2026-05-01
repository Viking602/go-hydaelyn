package core

func activeLeaseKey(runID, taskID string) string {
	return runID + "\x00" + taskID
}
