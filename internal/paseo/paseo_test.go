package paseo

import "testing"

func TestIsTitleGenerationMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "title generation response", message: `{"title":"Run repeated test chats"}`, want: true},
		{name: "empty title", message: `{"title":"  "}`, want: false},
		{name: "normal JSON response", message: `{"title":"Run repeated test chats","detail":"done"}`, want: false},
		{name: "ordinary reply", message: "任务已经完成", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTitleGenerationMessage(tt.message); got != tt.want {
				t.Errorf("IsTitleGenerationMessage(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
