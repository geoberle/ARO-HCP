using '../modules/fleet/fleet-registration-lookup.bicep'

param msiName = '{{ .fleet.managedIdentityName }}'
param regionalResourceGroup = '{{ .regionRG }}'
param rpCosmosDbName = '{{ .frontend.cosmosDB.name }}'
param cxDnsZoneName = '{{ .dns.regionalSubdomain }}.{{ .dns.cxParentZoneName }}'
