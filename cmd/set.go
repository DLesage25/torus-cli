package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli"

	"github.com/manifoldco/torus-cli/api"
	"github.com/manifoldco/torus-cli/apitypes"
	"github.com/manifoldco/torus-cli/config"
	"github.com/manifoldco/torus-cli/errs"
	"github.com/manifoldco/torus-cli/hints"
	"github.com/manifoldco/torus-cli/pathexp"
)

var setUnsetFlags = []cli.Flag{
	stdOrgFlag,
	stdProjectFlag,
	newSlicePlaceholder("environment, e", "ENV", "Use this environment.",
		"", "TORUS_ENVIRONMENT", true),
	newSlicePlaceholder("service, s", "SERVICE", "Use this service.",
		"default", "TORUS_SERVICE", true),
	newSlicePlaceholder("user, u", "USER", "Use this user.", "*", "TORUS_USER", false),
	newSlicePlaceholder("machine, m", "MACHINE", "Use this machine.", "*", "TORUS_MACHINE", false),
	newSlicePlaceholder("instance, i", "INSTANCE", "Use this instance.",
		"*", "TORUS_INSTANCE", true),
}

func init() {
	set := cli.Command{
		Name:      "set",
		Usage:     "Set a secret for a service and environment",
		ArgsUsage: "<name|path> <value> or <name|path>=<value>",
		Category:  "SECRETS",
		Flags:     setUnsetFlags,
		Action: chain(
			ensureDaemon, ensureSession, loadDirPrefs, loadPrefDefaults,
			setSliceDefaults, setCmd,
		),
	}

	Cmds = append(Cmds, set)
}

func setCmd(ctx *cli.Context) error {
	key, value, errMsg := parseSetArgs(ctx.Args())
	if errMsg != "" {
		return errs.NewUsageExitError(errMsg, ctx)
	}

	cred, err := setCredential(ctx, key, func() *apitypes.CredentialValue {
		return apitypes.NewStringCredentialValue(value)
	})

	if err != nil {
		return errs.NewErrorExitError("Could not set credential.", err)
	}

	name := (*cred.Body).GetName()
	pe := (*cred.Body).GetPathExp()
	fmt.Printf("\nCredential %s has been set at %s/%s\n", name, pe, name)

	hints.Display(hints.View, hints.Run)
	return nil
}

func parseSetArgs(args []string) (key string, value string, errMsg string) {
	// If there is only one argument, try to parse using env var syntax: KEY=value
	if len(args) == 1 {
		args = strings.SplitN(args[0], "=", 2)
	}

	switch len(args) {
	case 0, 1:
		errMsg = "A secret name and value must be supplied."
	case 2:
		key = args[0]
		value = args[1]

		if key == "" || value == "" {
			errMsg = "A secret must have a name and value."
		}
	default:
		errMsg = "Too many arguments were provided."
	}

	return key, value, errMsg
}

func determineCredential(ctx *cli.Context, nameOrPath string) (*pathexp.PathExp, *string, error) {
	// First try and use the cli args as a full path. it should override any
	// options.
	idx := strings.LastIndex(nameOrPath, "/")
	name := nameOrPath[idx+1:]

	var pe *pathexp.PathExp

	// It looks like the user gave a path expression. use that instead of flags.
	if idx != -1 {
		var err error
		path := nameOrPath[:idx]
		pe, err = pathexp.ParsePartial(path)
		if err != nil {
			return nil, nil, errs.NewExitError(err.Error())
		}
		if name == "*" {
			return nil, nil, errs.NewExitError("Secret name cannot be wildcard")
		}
	} else {
		// Falling back to flags. do the expensive population of the user flag now,
		// and see if any required flags (all of them) are missing.
		err := chain(setUserEnv, checkRequiredFlags)(ctx)
		if err != nil {
			return nil, nil, err
		}

		identity, err := deriveIdentitySlice(ctx)
		if err != nil {
			return nil, nil, err
		}

		pe, err = pathexp.New(
			ctx.String("org"),
			ctx.String("project"),
			ctx.StringSlice("environment"),
			ctx.StringSlice("service"),
			identity,
			ctx.StringSlice("instance"),
		)
		if err != nil {
			return nil, nil, err
		}
	}

	return pe, &name, nil
}

func setCredential(ctx *cli.Context, nameOrPath string, valueMaker func() *apitypes.CredentialValue) (*apitypes.CredentialEnvelope, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	client := api.NewClient(cfg)
	c := context.Background()

	pe, credName, err := determineCredential(ctx, nameOrPath)
	if err != nil {
		return nil, err
	}
	var name string
	if credName != nil {
		name = *credName
	}

	org, err := client.Orgs.GetByName(c, pe.Org.String())
	if org == nil || err != nil {
		return nil, errs.NewExitError("Org not found")
	}

	pName := pe.Project.String()
	projects, err := listProjects(&c, client, org.ID, &pName)
	if len(projects) != 1 || err != nil {
		return nil, errs.NewExitError("Project not found")
	}
	project := projects[0]
	value := valueMaker()

	state := "set"
	if value.IsUnset() {
		state = "unset"
		value = nil
	}

	var cred apitypes.Credential
	cBodyV2 := apitypes.CredentialV2{
		BaseCredential: apitypes.BaseCredential{
			OrgID:     org.ID,
			ProjectID: project.ID,
			Name:      strings.ToLower(name),
			PathExp:   pe,
			Value:     value,
		},
		State: state,
	}
	cred = &cBodyV2

	return client.Credentials.Create(c, &cred, progress)
}
