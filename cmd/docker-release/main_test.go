package main

import "testing"

func TestParseReleaseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    releaseOptions
		wantErr bool
	}{
		{
			name: "empty",
		},
		{
			name: "force",
			args: []string{"--force"},
			want: releaseOptions{force: true},
		},
		{
			name: "detach",
			args: []string{"--detach"},
			want: releaseOptions{detach: true},
		},
		{
			name: "short detach with force",
			args: []string{"-d", "--force"},
			want: releaseOptions{force: true, detach: true},
		},
		{
			name:    "unknown",
			args:    []string{"--wait"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReleaseOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
