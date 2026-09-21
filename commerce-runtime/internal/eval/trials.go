package eval

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ExpandTrials expands a dataset without claiming process independence. A
// suite runner must assign TrialIsolationID after creating its fresh runtime.
func ExpandTrials(scenarios []Scenario, trials int) ([]Scenario, error) {
	if trials <= 0 {
		return nil, errors.New("trials must be positive")
	}
	if trials == 1 {
		return scenarios, nil
	}
	result := make([]Scenario, 0, len(scenarios)*trials)
	for _, scenario := range scenarios {
		taskID := scenario.TaskID
		if taskID == "" {
			taskID = scenario.CaseID
		}
		for trial := 0; trial < trials; trial++ {
			copy := scenario
			copy.TaskID = taskID
			copy.TrialIndex = trial + 1
			copy.CaseID = fmt.Sprintf("%s#trial-%d", scenario.CaseID, trial+1)
			copy.Seed = scenario.Seed + int64((trial+1)*1000003)
			if len(copy.Request) > 0 {
				var request map[string]any
				if err := json.Unmarshal(copy.Request, &request); err != nil {
					return nil, fmt.Errorf("trial request %s: %w", copy.CaseID, err)
				}
				request["request_id"] = fmt.Sprintf("%s-trial-%d", requestID(copy.Request), trial+1)
				encoded, err := json.Marshal(request)
				if err != nil {
					return nil, err
				}
				copy.Request = encoded
			}
			copy.TrialIsolationID = ""
			result = append(result, copy)
		}
	}
	return result, nil
}
