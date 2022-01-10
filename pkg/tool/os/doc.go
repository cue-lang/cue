// Code generated by cue get go. DO NOT EDIT.

// Package os defines tasks for retrieving os-related information.
//
// CUE definitions:
//
//     // A Value are all possible values allowed in flags.
//     // A null value unsets an environment variable.
//     Value: bool | number | *string | null
//
//     // Name indicates a valid flag name.
//     Name: !="" & !~"^[$]"
//
//     // Setenv defines a set of command line flags, the values of which will be set
//     // at run time. The doc comment of the flag is presented to the user in help.
//     //
//     // To define a shorthand, define the shorthand as a new flag referring to
//     // the flag of which it is a shorthand.
//     Setenv: {
//         $id: "tool/os.Setenv"
//
//         {[Name]: Value}
//     }
//
//     // Getenv gets and parses the specific command line variables.
//     Getenv: {
//         $id: "tool/os.Getenv"
//
//         {[Name]: Value}
//     }
//
//     // Environ populates a struct with all environment variables.
//     Environ: {
//         $id: "tool/os.Environ"
//
//         // A map of all populated values.
//         // Individual entries may be specified ahead of time to enable
//         // validation and parsing. Values that are marked as required
//         // will fail the task if they are not found.
//         {[Name]: Value}
//     }
//
//     // Clearenv clears all environment variables.
//     Clearenv: {
//         $id: "tool/os.Clearenv"
//     }
//
//     // Mkdir creates a new directory at the specified path
//     Mkdir: {
//         $id: "tool/os.Mkdir"
//
//         // The directory path to create
//         // If path is already a directory, Mkdir does nothing
//         path: string
//
//         // When true any necessary parents are created as well
//         createParents: *false | bool
//     }
//
package os
