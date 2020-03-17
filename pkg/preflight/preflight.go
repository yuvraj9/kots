package preflight

import (
	"database/sql"

	"github.com/pkg/errors"
	"github.com/replicatedhq/kotsadm/pkg/downstream"
	"github.com/replicatedhq/kotsadm/pkg/kotsutil"
	"github.com/replicatedhq/kotsadm/pkg/logger"
	"github.com/replicatedhq/kotsadm/pkg/persistence"
	registrytypes "github.com/replicatedhq/kotsadm/pkg/registry/types"
	"github.com/replicatedhq/kotsadm/pkg/render"
)

func Run(appID string, sequence int64, archiveDir string) error {
	renderedKotsKinds, err := kotsutil.LoadKotsKindsFromPath(archiveDir)
	if err != nil {
		return errors.Wrap(err, "failed to load rendered kots kinds")
	}

	if renderedKotsKinds.Preflight != nil {
		// set the status to pending_preflights
		if err := downstream.SetDownstreamVersionPendingPreflight(appID, int64(sequence)); err != nil {
			return errors.Wrap(err, "failed to set downstream version pending preflight")
		}

		// render the preflight file
		// we need to convert to bytes first, so that we can reuse the renderfile function
		renderedMarshalledPreflights, err := renderedKotsKinds.Marshal("troubleshoot.replicated.com", "v1beta1", "Preflight")
		if err != nil {
			return errors.Wrap(err, "failed to marshal rendered preflight")
		}

		registrySettings, err := getRegistrySettingsForApp(appID)
		if err != nil {
			return errors.Wrap(err, "failed to get registry settings for app")
		}

		renderedPreflight, err := render.RenderFile(renderedKotsKinds, registrySettings, []byte(renderedMarshalledPreflights))
		if err != nil {
			return errors.Wrap(err, "failed to render preflights")
		}
		p, err := kotsutil.LoadPreflightFromContents(renderedPreflight)
		if err != nil {
			return errors.Wrap(err, "failed to load rendered preflight")
		}

		go func() {
			logger.Debug("preflight checks beginning")
			if err := execute(appID, sequence, p); err != nil {
				logger.Error(err)
				return
			}

			logger.Debug("preflight checks completed")
		}()
	} else {
		if err := downstream.SetDownstreamVersionReady(appID, int64(sequence)); err != nil {
			return errors.Wrap(err, "failed to set downstream version ready")
		}
	}

	return nil
}

// this is a copy from registry.  so many import cycles to unwind here, todo
func getRegistrySettingsForApp(appID string) (*registrytypes.RegistrySettings, error) {
	db := persistence.MustGetPGSession()
	query := `select registry_hostname, registry_username, registry_password_enc, namespace from app where id = $1`
	row := db.QueryRow(query, appID)

	var registryHostname sql.NullString
	var registryUsername sql.NullString
	var registryPasswordEnc sql.NullString
	var registryNamespace sql.NullString

	if err := row.Scan(&registryHostname, &registryUsername, &registryPasswordEnc, &registryNamespace); err != nil {
		return nil, errors.Wrap(err, "failed to scan registry")
	}

	if !registryHostname.Valid {
		return nil, nil
	}

	registrySettings := registrytypes.RegistrySettings{
		Hostname:    registryHostname.String,
		Username:    registryUsername.String,
		PasswordEnc: registryPasswordEnc.String,
		Namespace:   registryNamespace.String,
	}

	return &registrySettings, nil
}
