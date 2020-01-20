package sinkworker

import (
	"context"
	"github.com/chrisjohnson/azure-key-vault-agent/config"
	"github.com/chrisjohnson/azure-key-vault-agent/templateparser"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/chrisjohnson/azure-key-vault-agent/certs"
	"github.com/chrisjohnson/azure-key-vault-agent/keys"
	"github.com/chrisjohnson/azure-key-vault-agent/resource"
	"github.com/chrisjohnson/azure-key-vault-agent/secrets"
	"github.com/jpillora/backoff"
)

const RetryBreakPoint = 60

func Worker(ctx context.Context, cfg config.SinkConfig) {
	b := &backoff.Backoff{
		Min:    time.Duration(cfg.TimeFrequency),
		Max:    time.Duration(cfg.TimeFrequency) * 10,
		Factor: 2,
		Jitter: true,
	}

	d := b.Duration()
	ticker := time.NewTicker(d)

	log.Printf("Starting worker of kind %v for %v with refresh %v\n", cfg.Kind, cfg.Name, d)

	err := process(ctx, cfg)
	if err != nil {
		log.Printf("Failed to get resource: %v\n", err.Error())
	}

	for {
		select {
		case <-ctx.Done():
			// The main thread has cancelled the worker
			log.Println("Shutting down worker for: ", cfg.Name)
			return
		case <-ticker.C:
			log.Printf("Polling for worker %v\n", cfg.Name)
			err := process(ctx, cfg)
			if err != nil {
				if cfg.TimeFrequency > RetryBreakPoint {
					// For long frequencies, we should set up an explicit retry (with backoff)
					d := b.Duration()
					ticker = time.NewTicker(d)
					log.Println(err)
					log.Printf("Failed to get resource %v, will retry in %v\n", cfg.Name, d)
				} else {
					// For short frequencies, we can just wait for the next natural tick
					log.Printf("Failed to get resource: %v\n", err.Error())
				}
			} else {
				// Reset the ticker once we've got a good result
				if cfg.TimeFrequency > RetryBreakPoint {
					b.Reset()
					d := b.Duration()
					ticker = time.NewTicker(d)
					log.Printf("Success for resource %v, will try next in %v\n", cfg.Name, d)
				}
			}
		}
	}
}

var count int

func process(ctx context.Context, cfg config.SinkConfig) error {
	result, err := fetch(ctx, cfg)
	count++
	if count > 2 && count < 8 {
		//return errors.New("FAKE ERROR FROM AZURE")
	}
	if err != nil {
		return err
	}
	log.Printf("Got resource of kind %v for %v\n", cfg.Kind, cfg.Name)

	// Get old content
	oldContent := getOldContent(cfg)

	// Get new content
	newContent := getNewContent(cfg, result)

	// If a change was detected run pre/post commands and write the new file
	if oldContent != newContent {
		if cfg.PreChange != "" {
			err := runCommand(cfg.PreChange)
			if err != nil {
				log.Printf("PreChange command errored: %v", err)
			}
		}

		write(cfg, newContent)

		if cfg.PostChange != "" {
			err := runCommand(cfg.PostChange)
			if err != nil {
				log.Printf("PostChange command errored: %v", err)
			}
		}
	} else {
		log.Printf("No change detected for %v", cfg.Path)
	}

	return nil
}

func fetch(ctx context.Context, cfg config.SinkConfig) (result resource.Resource, err error) {
	switch cfg.Kind {
	case config.CertKind:
		result, err = certs.GetCert(cfg.VaultBaseURL, cfg.Name, cfg.Version)

	case config.SecretKind:
		result, err = secrets.GetSecret(cfg.VaultBaseURL, cfg.Name, cfg.Version)

	case config.KeyKind:
		result, err = keys.GetKey(cfg.VaultBaseURL, cfg.Name, cfg.Version)

	default:
		log.Panicf("Invalid sink kind: %v\n", cfg.Kind)
	}

	if err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func getNewContent(cfg config.SinkConfig, result resource.Resource) string {
	// If we have templates get the new value from rendering them
	if cfg.Template != "" || cfg.TemplatePath != "" {
		if cfg.Template != "" {
			// Execute inline template
			return templateparser.InlineTemplate(cfg.Template, cfg.Path, result)
		} else {
			// Execute template file
			return templateparser.TemplateFile(cfg.TemplatePath, cfg.Path, result)
		}
	} else {
		// Just return the secret string
		return result.String()
	}
}

func getOldContent(cfg config.SinkConfig) string {
	// If path has changed it will not yet exist so return empty string
	if _, err := os.Stat(cfg.Path); err != nil {
		if os.IsNotExist(err) {
			return ""
		}
	}

	// Read the contents of the current file into a string
	b, err := ioutil.ReadFile(cfg.Path)
	if err != nil {
		log.Panic(err)
	}

	return string(b)
}

func write(cfg config.SinkConfig, content string) {
	log.Printf("Writing %v to %v", cfg.Kind, cfg.Path)
	f, err := os.Create(cfg.Path)

	if err != nil {
		log.Panic(err)
	}

	defer f.Close()

	_, err = f.WriteString(content)

	if err != nil {
		log.Panic(err)
	}
}

func runCommand(command string) error {
	log.Printf("Executing %v", command)
	cmd := exec.Command("sh", "-c", command)

	err := cmd.Run()
	return err
}
