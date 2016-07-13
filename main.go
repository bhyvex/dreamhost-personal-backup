package main

import (
	"log"
	"os"
	"strings"
	"sync"

	"github.com/minio/minio-go"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ptrimble/dreamhost-personal-backup/backup"
	"github.com/ptrimble/dreamhost-personal-backup/backup/logger"
	"github.com/ptrimble/dreamhost-personal-backup/backup/worker"
)

func main() {
	processVars()

	s3Client, err := minio.NewV2(
		viper.GetString("s3Host"),
		viper.GetString("s3AccessKey"),
		viper.GetString("s3SecretKey"),
		false,
	)
	if err != nil {
		panic(err)
	}

	var workerWg sync.WaitGroup
	remoteActionChan := make(chan backup.RemoteAction, 20)

	reportChan := make(chan backup.LogEntry)
	reportOut := log.New(os.Stdout, "REPORT: ", log.Ldate|log.Ltime|log.LUTC)
	reportGenerator := backup.NewReporter(reportChan, reportOut)
	go reportGenerator.Run()

	logger := logger.NewLogger(os.Stdout, reportChan, &workerWg)

	targetDirs := strings.Split(viper.GetString("targetDirs"), ",")
	localFileProcessors := make([]backup.FileGatherer, len(targetDirs))
	for i, targetDir := range targetDirs {
		p := backup.NewLocalFileProcessor(targetDir)
		localFileProcessors[i] = &p
	}

	remoteFileProcessor, err := backup.NewRemoteFileProcessor(
		viper.GetString("s3BucketName"),
		s3Client.ListObjects,
		s3Client.RemoveObject,
		s3Client.FPutObject,
	)
	if err != nil {
		panic(err)
	}

	for i := 0; i < viper.GetInt("remoteWorkerCount"); i++ {
		go worker.NewRemoteActionWorker(
			remoteFileProcessor.Put,
			remoteFileProcessor.Remove,
			&workerWg,
			remoteActionChan,
			logger,
		).Run()
	}

	processor := backup.NewProcessor(
		localFileProcessors,
		&remoteFileProcessor,
		logger,
		&workerWg,
		remoteActionChan,
	)

	err = processor.Process()
	if err != nil {
		panic(err)
	}

	workerWg.Wait()
	reportGenerator.Print()
}

func processVars() {
	flag.String("targetDirs", "", "Local directories  to back up.")
	flag.String("s3Host", "", "S3 host.")
	flag.String("s3AccessKey", "", "S3 access key.")
	flag.String("s3SecretKey", "", "S3 secret key.")
	flag.String("s3BucketName", "", "S3 Bucket Name.")
	flag.Int("remoteWorkerCount", 0, "Number of workers performing actions against S3 host.")
	flag.Parse()

	viper.BindPFlag("targetDirs", flag.CommandLine.Lookup("targetDirs"))
	viper.BindPFlag("s3Host", flag.CommandLine.Lookup("s3Host"))
	viper.BindPFlag("s3AccessKey", flag.CommandLine.Lookup("s3AccessKey"))
	viper.BindPFlag("s3SecretKey", flag.CommandLine.Lookup("s3SecretKey"))
	viper.BindPFlag("s3BucketName", flag.CommandLine.Lookup("s3BucketName"))
	viper.BindPFlag("remoteWorkerCount", flag.CommandLine.Lookup("remoteWorkerCount"))

	viper.AutomaticEnv()
	viper.SetEnvPrefix("PERSONAL_BACKUP")
	viper.BindEnv("targetDirs")
	viper.BindEnv("s3Host")
	viper.BindEnv("s3AccessKey")
	viper.BindEnv("s3SecretKey")
	viper.BindEnv("s3BucketName")
	viper.BindEnv("remoteWorkerCount")

	viper.SetDefault("remoteWorkerCount", 5)
}
