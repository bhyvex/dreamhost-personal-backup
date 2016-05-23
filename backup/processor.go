package backup

import (
	"fmt"
	"sync"
)

type gatherResult struct {
	result map[string]file
	err    error
}

type processor struct {
	gatherLocalFiles  func() (map[string]file, error)
	gatherRemoteFiles func() (map[string]file, error)
	putToRemote       func(string) error
	removeFromRemote  func(string) error
	log               chan<- logEntry
}

func NewProcessor(
	localGather, remoteGather func() (map[string]file, error),
	putToRemote, removeFromRemote func(string) error,
	log chan<- logEntry,
) processor {
	return processor{
		gatherLocalFiles:  localGather,
		gatherRemoteFiles: remoteGather,
		putToRemote:       putToRemote,
		removeFromRemote:  removeFromRemote,
		log:               log,
	}
}

func (p processor) Process() (err error) {
	localResultChan := make(chan gatherResult)
	remoteResultChan := make(chan gatherResult)

	go func(out chan<- gatherResult) {
		files, err := p.gatherLocalFiles()
		out <- gatherResult{result: files, err: err}
	}(localResultChan)

	go func(out chan<- gatherResult) {
		files, err := p.gatherRemoteFiles()
		out <- gatherResult{result: files, err: err}
	}(remoteResultChan)

	local := <-localResultChan
	if local.err != nil {
		p.log <- logEntry{
			message: fmt.Sprintf("error returned while gathering local files, err: %s", local.err),
		}

		return local.err
	}

	remote := <-remoteResultChan
	if remote.err != nil {
		p.log <- logEntry{
			message: fmt.Sprintf("error returned while gathering remote files, err: %s", remote.err),
		}

		return remote.err
	}

	defer close(localResultChan)
	defer close(remoteResultChan)

	var wg sync.WaitGroup
	wg.Add(2)

	go p.processLocalVsRemote(local.result, remote.result, &wg)
	go p.processRemoteVsLocal(local.result, remote.result, &wg)

	wg.Wait()

	return
}

func (p processor) processLocalVsRemote(local, remote map[string]file, wg *sync.WaitGroup) {
	defer wg.Done()

	for lkey, lfile := range local {
		rfile, found := remote[lkey]
		if !found || !isEqual(lfile, rfile) {
			p.putToRemote(lkey)
		}
	}
}

func (p processor) processRemoteVsLocal(local, remote map[string]file, wg *sync.WaitGroup) {
	defer wg.Done()

	for rkey, _ := range remote {
		_, found := local[rkey]
		if !found {
			p.removeFromRemote(rkey)
		}
	}
}
