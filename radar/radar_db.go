package radar

import (
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
)

//go:generate counterfeiter . RadarDB

type RadarDB interface {
	GetPipelineName() string
	GetPipelineID() int
	ScopedName(string) string
	TeamID() int

	IsPaused() (bool, error)

	GetConfig() (atc.Config, db.ConfigVersion, bool, error)

	GetLatestVersionedResource(resourceName string) (db.SavedVersionedResource, bool, error)
	GetResource(resourceName string) (db.SavedResource, bool, error)
	GetResourceType(resourceTypeName string) (db.SavedResourceType, bool, error)
	PauseResource(resourceName string) error
	UnpauseResource(resourceName string) error

	SaveResourceVersions(atc.ResourceConfig, []atc.Version) error
	SaveResourceTypeVersion(atc.ResourceType, atc.Version) error
	SetResourceCheckError(resource db.SavedResource, err error) error
	AcquireResourceCheckingLock(logger lager.Logger, resource string, interval time.Duration, immediate bool) (db.Lock, bool, error)
	AcquireResourceTypeCheckingLock(logger lager.Logger, resourceType string, interval time.Duration, immediate bool) (db.Lock, bool, error)
}
