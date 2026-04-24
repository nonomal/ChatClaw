package channels

import "testing"

func TestShouldAutoConnectChannelOnStartup(t *testing.T) {
	tests := []struct {
		name string
		ch   Channel
		want bool
	}{
		{
			name: "auto connects normal gateway channel",
			ch: Channel{
				ConnectionType: ConnTypeGateway,
			},
			want: true,
		},
		{
			name: "auto connects legacy blank connection type",
			ch: Channel{
				ConnectionType: "",
			},
			want: true,
		},
		{
			name: "skips openclaw managed channel",
			ch: Channel{
				ConnectionType: ConnTypeGateway,
				OpenClawScope:  true,
			},
			want: false,
		},
		{
			name: "skips non gateway connection types",
			ch: Channel{
				ConnectionType: ConnTypeWebhook,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAutoConnectChannelOnStartup(tt.ch); got != tt.want {
				t.Fatalf("shouldAutoConnectChannelOnStartup(%+v) = %v, want %v", tt.ch, got, tt.want)
			}
		})
	}
}
