# Manual certificates

`POST /api/certs/create` accepts the existing multipart fields and aliases for
the certificate, private key, and optional intermediate certificate.

After Nginx Proxy Manager validates, creates, and uploads the certificate, the
endpoint returns NPM's post-upload certificate object. The response always
contains `id` and includes metadata such as `domain_names` and `expires_on`
when NPM provides it:

```json
{
  "id": 31,
  "nice_name": "manual cert",
  "provider": "other",
  "domain_names": ["example.com"],
  "expires_on": "2037-03-04 05:06:07"
}
```

If the successful upload has no certificate object, the server performs one
metadata lookup. A failed lookup does not undo the successful upload; in that
case the response falls back to `{"id": 31}`.
