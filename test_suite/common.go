package main

import (
"fmt"
"os/exec"
"time"
)

// stopNode4 停止第四个节点
func stopNode4() {
printInfo("停止Node4...")
cmd := exec.Command("bash", "-c", fmt.Sprintf("cd %s && ./start.sh stop node4", burinPath))
cmd.Run()
time.Sleep(2 * time.Second)
printSuccess("Node4已停止")
}
