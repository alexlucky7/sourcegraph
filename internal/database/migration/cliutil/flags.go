package cliutil

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/peterbourgon/ff/v3/ffcli"

	"github.com/sourcegraph/sourcegraph/internal/database/migration/runner"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/schemas"
	"github.com/sourcegraph/sourcegraph/lib/output"
)

type RunFunc func(ctx context.Context, options runner.Options) error

func Flags(commandName string, run RunFunc, out *output.Output) *ffcli.Command {
	rootFlagSet := flag.NewFlagSet(commandName, flag.ExitOnError)

	return &ffcli.Command{
		Name:       commandName,
		ShortUsage: fmt.Sprintf("%s <command>", commandName),
		ShortHelp:  "Modifies and runs schema migrations",
		FlagSet:    rootFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ffcli.Command{
			Up(commandName, run, out),
			UpTo(commandName, run, out),
			Undo(commandName, run, out),
			DownTo(commandName, run, out),
		},
	}
}

func Up(commandName string, run RunFunc, out *output.Output) *ffcli.Command {
	var (
		flagSet        = flag.NewFlagSet(fmt.Sprintf("%s up", commandName), flag.ExitOnError)
		schemaNameFlag = flagSet.String("db", "all", `The target schema(s) to migrate. Comma-separated values are accepted. Supply "all" (the default) to migrate all schemas.`)
	)

	exec := func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: too many arguments"))
			return flag.ErrHelp
		}

		schemaNames := strings.Split(*schemaNameFlag, ",")
		if len(schemaNames) == 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: supply a schema via -db"))
			return flag.ErrHelp
		}
		if len(schemaNames) == 1 && schemaNames[0] == "all" {
			schemaNames = schemas.SchemaNames
		}

		operations := []runner.MigrationOperation{}
		for _, schemaName := range schemaNames {
			operations = append(operations, runner.MigrationOperation{
				SchemaName: schemaName,
				Type:       runner.MigrationOperationTypeTargetedUpgrade,
			})
		}

		return run(ctx, runner.Options{
			Operations: operations,
		})
	}

	return &ffcli.Command{
		Name:       "up",
		ShortUsage: fmt.Sprintf("%s up -db=<schema>", commandName),
		ShortHelp:  "Apply all migrations",
		FlagSet:    flagSet,
		Exec:       exec,
		LongHelp:   ConstructLongHelp(),
	}
}

func UpTo(commandName string, run RunFunc, out *output.Output) *ffcli.Command {
	var (
		flagSet        = flag.NewFlagSet(fmt.Sprintf("%s upto", commandName), flag.ExitOnError)
		schemaNameFlag = flagSet.String("db", "", `The target schema to migrate.`)
		targetsFlag    = flagSet.String("target", "", "The migration to apply. Comma-separated values are accepted. ")
	)

	exec := func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: too many arguments"))
			return flag.ErrHelp
		}

		if *schemaNameFlag == "" {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: supply a schema via -db"))
			return flag.ErrHelp
		}

		targets := strings.Split(*targetsFlag, ",")
		if len(targets) == 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: supply a migration target via -target"))
			return flag.ErrHelp
		}

		versions := make([]int, 0, len(targets))
		for _, target := range targets {
			version, err := strconv.Atoi(target)
			if err != nil {
				return err
			}

			versions = append(versions, version)
		}

		return run(ctx, runner.Options{
			Operations: []runner.MigrationOperation{
				{
					SchemaName:     *schemaNameFlag,
					Type:           runner.MigrationOperationTypeTargetedUp,
					TargetVersions: versions,
				},
			},
		})
	}

	return &ffcli.Command{
		Name:       "upto",
		ShortUsage: fmt.Sprintf("%s upto -db=<schema> -target=<target>,<target>,...", commandName),
		ShortHelp:  "Ensure a given migration has been applied - may apply dependency migrations",
		FlagSet:    flagSet,
		Exec:       exec,
		LongHelp:   ConstructLongHelp(),
	}
}

func Undo(commandName string, run RunFunc, out *output.Output) *ffcli.Command {
	var (
		flagSet        = flag.NewFlagSet(fmt.Sprintf("%s undo", commandName), flag.ExitOnError)
		schemaNameFlag = flagSet.String("db", "", `The target schema to migrate.`)
	)

	exec := func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: too many arguments"))
			return flag.ErrHelp
		}

		if *schemaNameFlag == "" {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: supply a schema via -db"))
			return flag.ErrHelp
		}

		return run(ctx, runner.Options{
			Operations: []runner.MigrationOperation{
				{
					SchemaName: *schemaNameFlag,
					Type:       runner.MigrationOperationTypeTargetedRevert,
				},
			},
		})
	}

	return &ffcli.Command{
		Name:       "undo",
		ShortUsage: fmt.Sprintf("%s undo -db=<schema>", commandName),
		ShortHelp:  `Revert the last migration applied - useful in local development`,
		FlagSet:    flagSet,
		Exec:       exec,
		LongHelp:   ConstructLongHelp(),
	}
}

func DownTo(commandName string, run RunFunc, out *output.Output) *ffcli.Command {
	var (
		flagSet        = flag.NewFlagSet(fmt.Sprintf("%s downto", commandName), flag.ExitOnError)
		schemaNameFlag = flagSet.String("db", "", `The target schema to migrate.`)
		targetsFlag    = flagSet.String("target", "", "Revert all children of the given target. Comma-separated values are accepted. ")
	)

	exec := func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: too many arguments"))
			return flag.ErrHelp
		}

		if *schemaNameFlag == "" {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: supply a schema via -db"))
			return flag.ErrHelp
		}

		targets := strings.Split(*targetsFlag, ",")
		if len(targets) == 0 {
			out.WriteLine(output.Linef("", output.StyleWarning, "ERROR: supply a migration target via -target"))
			return flag.ErrHelp
		}

		versions := make([]int, 0, len(targets))
		for _, target := range targets {
			version, err := strconv.Atoi(target)
			if err != nil {
				return err
			}

			versions = append(versions, version)
		}

		return run(ctx, runner.Options{
			Operations: []runner.MigrationOperation{
				{
					SchemaName:     *schemaNameFlag,
					Type:           runner.MigrationOperationTypeTargetedDown,
					TargetVersions: versions,
				},
			},
		})
	}

	return &ffcli.Command{
		Name:       "downto",
		ShortUsage: fmt.Sprintf("%s downto -db=<schema> -target=<target>,<target>,...", commandName),
		ShortHelp:  `Revert any applied migrations that are children of the given targets - this effectively "resets" the database to the target migration`,
		FlagSet:    flagSet,
		Exec:       exec,
		LongHelp:   ConstructLongHelp(),
	}
}

func ConstructLongHelp() string {
	names := make([]string, 0, len(schemas.SchemaNames))
	for _, name := range schemas.SchemaNames {
		names = append(names, fmt.Sprintf("  %s", name))
	}

	return fmt.Sprintf("AVAILABLE SCHEMAS\n%s", strings.Join(names, "\n"))
}
