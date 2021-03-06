package pkg

import (
	"fmt"
	"github.com/cyberark/secretless-broker/test/util/test"
	"os/exec"
	"strings"

	"github.com/smartystreets/goconvey/convey"
)


func RunQuery(clientConfig test.ClientConfiguration, connectPort test.ConnectionPort) (string, error) {
	args := []string{"-e", "select count(*) from testdb.test"}

	if clientConfig.SSL {
		args = append(args, "--ssl", "--ssl-verify-server-cert=TRUE")
	}
	if clientConfig.Username != "" {
		args = append(args, fmt.Sprintf("--user=%s", clientConfig.Username))
	}
	if clientConfig.Password != "" {
		args = append(args, fmt.Sprintf("--password=%s", clientConfig.Password))
	}
	switch connectPort.SocketType {
	case test.TCP:
		args = append(args, fmt.Sprintf("--host=%s", connectPort.Host()))
		args = append(args, fmt.Sprintf("--port=%s", connectPort.ToPortString()))
	case test.Socket:
		args = append(args, fmt.Sprintf("--socket=%s", connectPort.ToSocketPath()))
	default:
		panic("Listener Type can only be TCP or Socket")
	}

	// ensures mysql can handle non-native auth
	args = append(args, "--default-auth=mysql_clear_password")

	// Pre command logs
	convey.Println("")
	convey.Println("---<< EXECUTED")
	convey.Println(strings.Join(append([]string{"mysql"}, args...), " "))

	cmdOut, err := exec.Command("mysql", args...).CombinedOutput()

	// Post command logs
	//TODO: deal with verbose
	if test.Verbose {
		if err != nil {
			convey.Println("--->> RESULTS")
			convey.Println("----- ERROR: ")
			convey.Println(err.Error())
		}
		convey.Println("----- OUTPUT: ")
		convey.Println(string(cmdOut))
	}
	convey.Println("---<< END")
	convey.Println("")

	return string(cmdOut), err
}

