package brew

import "testing"

const sample = `[
  {"name":"mariadb","status":"started","user":"dev","file":"/x.plist","exit_code":null},
  {"name":"postgresql@18","status":"error","user":"dev","exit_code":1},
  {"name":"redis","status":"none","user":null,"exit_code":null,"log_path":"/opt/homebrew/var/log/redis.log"}
]`

func TestParseServices(t *testing.T) {
	list, err := ParseServices([]byte(sample))
	if err != nil || len(list) != 3 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].Status != "started" || list[1].ExitCode == nil || *list[1].ExitCode != 1 || list[2].LogPath == "" {
		t.Errorf("%+v", list)
	}
}
