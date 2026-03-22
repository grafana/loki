# List of projects to provide to the make-docs script.
# Format is PROJECT[:[VERSION][:[REPOSITORY][:[DIRECTORY]]]]
# This overrides the default behavior of assuming the repository directory is the same as the project name.
PROJECTS := loki:UNVERSIONED:$(notdir $(basename $(shell git rev-parse --show-toplevel)))
