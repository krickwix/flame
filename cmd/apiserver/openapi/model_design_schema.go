/*
 * Fledge REST API
 *
 * No description provided (generated by Openapi Generator https://github.com/openapitools/openapi-generator)
 *
 * API version: 1.0.0
 * Generated by: OpenAPI Generator (https://openapi-generator.tech)
 */

package openapi

type DesignSchema struct {

	Backend string `json:"backend"`

	Roles []Role `json:"roles"`

	Channels []Channel `json:"channels"`
}
