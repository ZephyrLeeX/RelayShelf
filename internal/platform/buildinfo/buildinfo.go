package buildinfo

import "runtime/debug"

// These are populated with -ldflags in release builds. Local builds fall back
// to Go module/VCS metadata instead of inventing a product version.
var (
	Version   = ""
	GitCommit = ""
	BuildTime = ""
)

type Info struct{ Version, GitCommit, BuildTime string }

func Current() Info {
	info := Info{Version: Version, GitCommit: GitCommit, BuildTime: BuildTime}
	if build, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" && build.Main.Version != "" && build.Main.Version != "(devel)" {
			info.Version = build.Main.Version
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.GitCommit == "" {
					info.GitCommit = setting.Value
				}
			case "vcs.time":
				if info.BuildTime == "" {
					info.BuildTime = setting.Value
				}
			}
		}
	}
	if info.Version == "" {
		info.Version = "development"
	}
	if info.GitCommit == "" {
		info.GitCommit = "unknown"
	}
	if info.BuildTime == "" {
		info.BuildTime = "unknown"
	}
	return info
}
