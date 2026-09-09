package main

// LoadPrunedFocusHistory loads witness-checked history and drops dead targets.
func LoadPrunedFocusHistory(snapshot HerdrSessionSnapshot, witness ServerContinuityWitness) (FocusHistory, error) {
	var history FocusHistory
	err := withFocusHistoryLock(pluginStateDir(), func() error {
		loaded, loadErr := LoadValidatedFocusHistory(pluginStateDir(), witness)
		if loadErr != nil {
			return loadErr
		}
		history = pruneFocusHistory(loaded, snapshot)
		return writeFocusHistoryFile(pluginStateDir(), history)
	})
	return history, err
}
