package renderer

import (
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

func TestGetVisibleNameForUserFallbacks(t *testing.T) {
	var manager peers.Manager
	userWithUsername := &tg.User{ID: 2}
	userWithUsername.SetUsername("janedoe")

	tests := []struct {
		name string
		user *tg.User
		want string
	}{
		{
			name: "visible name",
			user: &tg.User{ID: 1, FirstName: "Jane", LastName: "Doe"},
			want: "Jane Doe",
		},
		{
			name: "username",
			user: userWithUsername,
			want: "@janedoe",
		},
		{
			name: "deleted",
			user: &tg.User{ID: 3, Deleted: true},
			want: "<deleted user>",
		},
		{
			name: "id",
			user: &tg.User{ID: 4},
			want: "<user 4>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getVisibleName(manager.User(tt.user))
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
