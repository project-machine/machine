package main

import (
	"fmt"
	"os"

	"github.com/apex/log"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

var deleteCmd = cli.Command{
	Name:   "delete",
	Usage:  "delete a machine (cluster) from local system",
	Action: doDelete,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "force",
			Usage: "force delete a running cluster.",
		},
	},
}

func doDelete(ctx *cli.Context) error {
	if ctx.NArg() == 0 {
		msg := "Cluster name must be provided.  Use the 'list' operation to see what clusters are defined"
		return errors.Errorf(msg)
	}

	cluster := ctx.Args()[0]
	cPath := ConfPath(cluster)
	if !PathExists(cPath) {
		return errors.Errorf("Could not delete cluster %s, not found: %s", cluster, cPath)
	}

	// apiSock := ApiSockPath(cluster)
	// if PathExists(apiSock) {
	// 	fmt.Printf("Found socket: %s\n", apiSock)
	// 	if !ctx.Bool("force") {
	// 		return errors.Errorf("Cluster %s is running, not deleting. Use --force to kill.", cluster)
	// 	}
	// 	fmt.Printf("Cluster is running and force:%t\n", ctx.Bool("force"))
	// 	return nil
	// }

	if IsClusterRunning(cluster) {
		if !ctx.Bool("force") {
			return errors.Errorf("Cluster %s is running, not deleting. Use --force to kill.", cluster)
		}
		fmt.Printf("Cluster %s is running, user requested force delete.\n", cluster)
		_, err := ClusterStop(cluster)
		if err != nil {
			return errors.Errorf("Failed to stop cluster %s: %w\n", cluster, err)
		}
	}

	fmt.Printf("Removing cluster %s config %s\n", cluster, cPath)
	err := os.Remove(cPath)
	if err != nil {
		return errors.Errorf("Could not remove cluster %s configuration: %s", cluster, cPath)
	}
	dPath := DataDir(cluster)
	if PathExists(dPath) {
		fmt.Printf("Removing cluster %s data %s\n", cluster, dPath)
		err := os.RemoveAll(dPath)
		if err != nil {
			return errors.Errorf("Could not remove cluster %s data directory: %s", cluster, dPath)
		}
	}
	log.Infof("Deleted cluster %s (%s:%s)", cluster, cPath, dPath)
	return nil
}
