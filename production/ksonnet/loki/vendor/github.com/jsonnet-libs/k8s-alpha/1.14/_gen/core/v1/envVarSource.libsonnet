{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='envVarSource', url='', help='EnvVarSource represents a source for the value of an EnvVar.'),
  '#configMapKeyRef':: d.obj(help='Selects a key from a ConfigMap.'),
  configMapKeyRef: {
    '#withKey':: d.fn(help='The key to select.', args=[d.arg(name='key', type=d.T.string)]),
    withKey(key): { configMapKeyRef+: { key: key } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { configMapKeyRef+: { name: name } },
    '#withOptional':: d.fn(help="Specify whether the ConfigMap or it's key must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
    withOptional(optional): { configMapKeyRef+: { optional: optional } }
  },
  '#fieldRef':: d.obj(help='ObjectFieldSelector selects an APIVersioned field of an object.'),
  fieldRef: {
    '#withFieldPath':: d.fn(help='Path of the field to select in the specified API version.', args=[d.arg(name='fieldPath', type=d.T.string)]),
    withFieldPath(fieldPath): { fieldRef+: { fieldPath: fieldPath } }
  },
  '#resourceFieldRef':: d.obj(help='ResourceFieldSelector represents container resources (cpu, memory) and their output format'),
  resourceFieldRef: {
    '#withContainerName':: d.fn(help='Container name: required for volumes, optional for env vars', args=[d.arg(name='containerName', type=d.T.string)]),
    withContainerName(containerName): { resourceFieldRef+: { containerName: containerName } },
    '#withDivisor':: d.fn(help="Quantity is a fixed-point representation of a number. It provides convenient marshaling/unmarshaling in JSON and YAML, in addition to String() and Int64() accessors.\n\nThe serialization format is:\n\n<quantity>        ::= <signedNumber><suffix>\n  (Note that <suffix> may be empty, from the '' case in <decimalSI>.)\n<digit>           ::= 0 | 1 | ... | 9 <digits>          ::= <digit> | <digit><digits> <number>          ::= <digits> | <digits>.<digits> | <digits>. | .<digits> <sign>            ::= '+' | '-' <signedNumber>    ::= <number> | <sign><number> <suffix>          ::= <binarySI> | <decimalExponent> | <decimalSI> <binarySI>        ::= Ki | Mi | Gi | Ti | Pi | Ei\n  (International System of units; See: http://physics.nist.gov/cuu/Units/binary.html)\n<decimalSI>       ::= m | '' | k | M | G | T | P | E\n  (Note that 1024 = 1Ki but 1000 = 1k; I didn't choose the capitalization.)\n<decimalExponent> ::= 'e' <signedNumber> | 'E' <signedNumber>\n\nNo matter which of the three exponent forms is used, no quantity may represent a number greater than 2^63-1 in magnitude, nor may it have more than 3 decimal places. Numbers larger or more precise will be capped or rounded up. (E.g.: 0.1m will rounded up to 1m.) This may be extended in the future if we require larger or smaller quantities.\n\nWhen a Quantity is parsed from a string, it will remember the type of suffix it had, and will use the same type again when it is serialized.\n\nBefore serializing, Quantity will be put in 'canonical form'. This means that Exponent/suffix will be adjusted up or down (with a corresponding increase or decrease in Mantissa) such that:\n  a. No precision is lost\n  b. No fractional digits will be emitted\n  c. The exponent (or suffix) is as large as possible.\nThe sign will be omitted unless the number is negative.\n\nExamples:\n  1.5 will be serialized as '1500m'\n  1.5Gi will be serialized as '1536Mi'\n\nNote that the quantity will NEVER be internally represented by a floating point number. That is the whole point of this exercise.\n\nNon-canonical values will still parse as long as they are well formed, but will be re-emitted in their canonical form. (So always use canonical form, or don't diff.)\n\nThis format is intended to make it difficult to use these numbers without writing some sort of special handling code in the hopes that that will cause implementors to also use a fixed point implementation.", args=[d.arg(name='divisor', type=d.T.string)]),
    withDivisor(divisor): { resourceFieldRef+: { divisor: divisor } },
    '#withResource':: d.fn(help='Required: resource to select', args=[d.arg(name='resource', type=d.T.string)]),
    withResource(resource): { resourceFieldRef+: { resource: resource } }
  },
  '#secretKeyRef':: d.obj(help='SecretKeySelector selects a key of a Secret.'),
  secretKeyRef: {
    '#withKey':: d.fn(help='The key of the secret to select from.  Must be a valid secret key.', args=[d.arg(name='key', type=d.T.string)]),
    withKey(key): { secretKeyRef+: { key: key } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretKeyRef+: { name: name } },
    '#withOptional':: d.fn(help="Specify whether the Secret or it's key must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
    withOptional(optional): { secretKeyRef+: { optional: optional } }
  },
  '#mixin': 'ignore',
  mixin: self
}