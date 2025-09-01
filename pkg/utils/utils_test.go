package utils

import "testing"

func TestIsImageEqual(t *testing.T) {
	type args struct {
		image1 string
		image2 string
	}

	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "same image with version",
			args: args{
				image1: "aerospike/aerospike-server-enterprise:8.1",
				image2: "aerospike/aerospike-server-enterprise:8.1",
			},
			want: true,
		},
		{
			name: "same image without latest version",
			args: args{
				image1: "aerospike/aerospike-server-enterprise:latest",
				image2: "aerospike/aerospike-server-enterprise",
			},
			want: true,
		},
		{
			name: "same image without latest version",
			args: args{
				image1: "aerospike/aerospike-server-enterprise",
				image2: "aerospike/aerospike-server-enterprise:latest",
			},
			want: true,
		},
		{
			name: "same image without docker.io prefix",
			args: args{
				image1: "docker.io/aerospike/aerospike-server-enterprise:8.1",
				image2: "aerospike/aerospike-server-enterprise:8.1",
			},
			want: true,
		},
		{
			name: "different image with version",
			args: args{
				image1: "aerospike/aerospike-server-enterprise:8.1",
				image2: "aerospike/aerospike-server-enterprise:8.0",
			},
			want: false,
		},
		{
			name: "different image name",
			args: args{
				image1: "aerospike/aerospike-server-enterprise:8.1",
				image2: "aerospike/aerospike-server:8.1",
			},
			want: false,
		},
		{
			name: "same image with pull through cache registry",
			args: args{
				image1: "aerospike/aerospike-server-enterprise:8.1",
				image2: "000000000000.dkr.ecr.some-region.amazonaws.com/docker-hub/aerospike/aerospike-server-enterprise:8.1",
			},
			want: true,
		},
		{
			name: "same image with pull through cache registry",
			args: args{
				image1: "000000000000.dkr.ecr.some-region.amazonaws.com/docker-hub/aerospike/aerospike-server-enterprise:8.1",
				image2: "aerospike/aerospike-server-enterprise:8.1",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsImageEqual(tt.args.image1, tt.args.image2); got != tt.want {
				t.Errorf("IsImageEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDockerImageTag(t *testing.T) {
	type args struct {
		tag string
	}

	tests := []struct {
		name         string
		args         args
		wantRegistry string
		wantName     string
		wantVersion  string
	}{
		{
			name: "image without registry",
			args: args{
				tag: "aerospike/aerospike-server-enterprise:8.1",
			},
			wantRegistry: "index.docker.io",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "8.1",
		},
		{
			name: "image with registry",
			args: args{
				tag: "000000000000.dkr.ecr.some-region.amazonaws.com/docker-hub/aerospike/aerospike-server-enterprise:8.1",
			},
			wantRegistry: "000000000000.dkr.ecr.some-region.amazonaws.com",
			wantName:     "docker-hub/aerospike/aerospike-server-enterprise",
			wantVersion:  "8.1",
		},
		{
			name: "image without version",
			args: args{
				tag: "aerospike/aerospike-server-enterprise",
			},
			wantRegistry: "index.docker.io",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "latest",
		},
		{
			name: "empty image",
			args: args{
				tag: "",
			},
			wantRegistry: "",
			wantName:     "",
			wantVersion:  "",
		},
		{
			name: "version with digest",
			args: args{
				tag: "aerospike/aerospike-server-enterprise:8.1@sha256:abcdef",
			},
			wantRegistry: "index.docker.io",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "8.1@sha256:abcdef",
		},
		{
			name: "digest without version",
			args: args{
				tag: "aerospike/aerospike-server-enterprise@sha256:abcdef",
			},
			wantRegistry: "index.docker.io",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "latest@sha256:abcdef",
		},
		{
			name: "registry with port",
			args: args{
				tag: "my-registry.com:5000/aerospike/aerospike-server-enterprise:8.1",
			},
			wantRegistry: "my-registry.com:5000",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "8.1",
		},
		{
			name: "registry with ip",
			args: args{
				tag: "127.0.0.1:5000/aerospike/aerospike-server-enterprise:8.1",
			},
			wantRegistry: "127.0.0.1:5000",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "8.1",
		},
		{
			name: "localhost registry",
			args: args{
				tag: "localhost:5000/aerospike/aerospike-server-enterprise:8.1",
			},
			wantRegistry: "localhost:5000",
			wantName:     "aerospike/aerospike-server-enterprise",
			wantVersion:  "8.1",
		},
		{
			name: "library image",
			args: args{
				tag: "aerospike-server-enterprise:8.1",
			},
			wantRegistry: "index.docker.io",
			wantName:     "library/aerospike-server-enterprise",
			wantVersion:  "8.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRegistry, gotName, gotVersion := ParseDockerImageTag(tt.args.tag)
			if gotRegistry != tt.wantRegistry {
				t.Errorf("ParseDockerImageTag() gotRegistry = %v, want %v", gotRegistry, tt.wantRegistry)
			}

			if gotName != tt.wantName {
				t.Errorf("ParseDockerImageTag() gotName = %v, want %v", gotName, tt.wantName)
			}

			if gotVersion != tt.wantVersion {
				t.Errorf("ParseDockerImageTag() gotVersion = %v, want %v", gotVersion, tt.wantVersion)
			}
		})
	}
}
