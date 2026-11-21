package manage

import (
	"encoding/json"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Compilation runs without the publication lock. Merge operational records at
// commit so a completed delivery cannot be erased by a long configuration build.
// Cached compiler inputs remain exactly those used to validate the artifacts.
func publishPlatformFiles(root string, files fileBundle) error {
	var config sub.Config
	var runtime sub.Runtime
	if err := json.Unmarshal(files[subscriptionsConfig], &config); err != nil {
		return err
	}
	if err := json.Unmarshal(files[subscriptionsRuntime], &runtime); err != nil {
		return err
	}
	latest, err := loadSubscriptionRuntime(root)
	if err != nil {
		return err
	}
	runtime.Runs = latest.Runs
	runtime.Deliveries = sub.PruneRuntime(config, latest).Deliveries
	if err := files.subscriptionRuntime(config, runtime); err != nil {
		return err
	}
	return publish(root, files)
}
