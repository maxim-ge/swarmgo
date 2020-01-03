/*
 * Copyright (c) 2018-present unTill Pro, Ltd. and Contributors
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 *
 */

package swarmgo

import (
	"fmt"
	"path/filepath"
	"strings"

	gc "github.com/untillpro/gochips"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

const (
	eLKPrefix          = "elk"
	eLKComposeFileName = eLKPrefix + "/elk.yml"
)

// eLKCmd represents the elastic command
var eLKCmd = &cobra.Command{
	Use:   "elk",
	Short: "Deploy ELK stack",
	Long:  `Deploys Elasticsearch cluster with 3 nodes, Logstash replica, Filebeat on all nodes and single Kibana`,
	Run: func(cmd *cobra.Command, args []string) {
		initCommand("elk")
		defer finitCommand()
		passToKey := readKeyPassword()
		firstEntry, clusterFile := getSwarmLeaderNodeAndClusterFile()
		kibanaUser := readPasswordPrompt("Kibana login")
		kibanaPass := readPasswordPrompt("Kibana password")
		kibanaHashedPass := strings.Replace(hashPassword(kibanaPass), "$", "\\$\\$", -1)
		clusterFile.KibanaCreds = fmt.Sprintf("%s:%s", kibanaUser, kibanaHashedPass)
		if !firstEntry.node.Traefik {
			gc.Fatal("Need to deploy traefik before elk deploy")
		}
		deployELKStack(passToKey, clusterFile, firstEntry)
	},
}

func deployELKStack(passToKey string, clusterFile *clusterFile, firstEntry *entry) {
	host := firstEntry.node.Host
	config := findSSHKeysAndInitConnection(passToKey, clusterFile)
	forCopy := infoForCopy{
		firstEntry,
		config,
		clusterFile,
	}
	gc.Info("Trying to install dos2unix")
	sudoExecSSHCommand(host, "apt-get install dos2unix", config)
	curDir := getSourcesDir()
	copyToHost(&forCopy, filepath.ToSlash(filepath.Join(curDir, eLKPrefix)))
	appliedBuffer := executeTemplateToFile(eLKComposeFileName, clusterFile)
	execSSHCommand(host, "cat > ~/"+eLKComposeFileName+" << EOF\n\n"+
		appliedBuffer.String()+"\nEOF", config)
	gc.Info(eLKComposeFileName, "applied by template")
	gc.Info("Increasing vm.max_map_count")
	increaseVMMaxMapCount(passToKey, clusterFile)
	gc.Info("Increased")
	gc.Info("Trying to deploy ELK")
	sudoExecSSHCommand(host, "docker stack deploy -c "+eLKComposeFileName+" elk", config)
	gc.Info("ELK deployed")
}

func increaseVMMaxMapCount(passToKey string, clusterFile *clusterFile) {
	nodesFromYml := getNodesFromYml(getWorkingDir())
	doneChannel := make(chan interface{})
	for _, value := range nodesFromYml {
		go func(node node) {
			config := findSSHKeysAndInitConnection(passToKey, clusterFile)
			_, err := sudoExecSSHCommandWithoutPanic(node.Host, "sysctl -w vm.max_map_count=262144", config)
			if err != nil {
				doneChannel <- err
			}
			doneChannel <- struct{}{}
		}(value)
	}
	errors := make([]error, 0, len(nodesFromYml))
	for range nodesFromYml {
		out := <-doneChannel
		switch out.(type) {
		case error:
			errors = append(errors, out.(error))
		}
	}
	if len(errors) != 0 {
		gc.Fatal(errors)
	}
	close(doneChannel)
}

func hashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		panic(err)
	}
	return string(hash)
}
