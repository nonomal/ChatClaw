package openclawcron

import "testing"

func TestDeriveCronDeliveryChannelIDPrefersOpenClawChannelID(t *testing.T) {
	option := cronDeliveryChannelOption{
		Platform:    "qq",
		ExtraConfig: `{"openclaw_channel_id":"channel_4"}`,
	}

	if got := deriveCronDeliveryChannelID(option); got != "qqbot" {
		t.Fatalf("deriveCronDeliveryChannelID() = %q, want %q", got, "qqbot")
	}
}

func TestDeriveCronDeliveryChannelIDFallsBackToPlatform(t *testing.T) {
	option := cronDeliveryChannelOption{
		Platform: "wecom",
	}

	if got := deriveCronDeliveryChannelID(option); got != "wecom" {
		t.Fatalf("deriveCronDeliveryChannelID() = %q, want %q", got, "wecom")
	}
}

func TestDeriveCronDeliveryAccountIDPrefersOpenClawChannelID(t *testing.T) {
	option := cronDeliveryChannelOption{
		ID:          4,
		Platform:    "qq",
		ExtraConfig: `{"openclaw_channel_id":"channel_4"}`,
	}

	if got := deriveCronDeliveryAccountID(option); got != "channel_4" {
		t.Fatalf("deriveCronDeliveryAccountID() = %q, want %q", got, "channel_4")
	}
}

func TestFlattenJobNormalizesDeliveryChannelForUI(t *testing.T) {
	item := openClawJobStoreItem{}
	item.Delivery.Channel = "dingtalk-connector"

	job := flattenJob(item)
	if job.DeliveryChannel != "dingtalk" {
		t.Fatalf("flattenJob().DeliveryChannel = %q, want %q", job.DeliveryChannel, "dingtalk")
	}
}
