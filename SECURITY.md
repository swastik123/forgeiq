# Security Policy

## Supported Versions

We release patches for security vulnerabilities. Which versions are eligible for receiving such patches depends on the CVSS v3.0 Rating:

| Version | Supported          |
| ------- | ------------------ |
| 1.x.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

Please report security vulnerabilities by emailing security@forgeiq.dev (or your security email).

**Please do not report security vulnerabilities through public GitHub issues.**

### What to Include

- Type of issue (e.g., buffer overflow, SQL injection, cross-site scripting, etc.)
- Full paths of source file(s) related to the manifestation of the issue
- The location of the affected source code (tag/branch/commit or direct URL)
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue, including how an attacker might exploit the issue

### Response Timeline

- **Initial Response**: Within 48 hours
- **Status Update**: Within 7 days
- **Resolution**: Depends on severity and complexity

### Security Best Practices

When using ForgeIQ:

1. **Keep dependencies updated**: Regularly update Go modules
2. **Use HTTPS**: Always use HTTPS for external service connections
3. **Secure credentials**: Use secret management systems
4. **Validate inputs**: All inputs are validated, but verify your use cases
5. **Monitor logs**: Enable logging and monitor for suspicious activity
6. **Network security**: Use firewalls and network policies
7. **Regular audits**: Review access controls and permissions

### Known Security Considerations

- **API Keys**: Store in environment variables or secret managers
- **Database**: Use strong passwords and SSL connections
- **Temporal**: Secure Temporal cluster access
- **External Services**: Validate SSL certificates
- **Rate Limiting**: Configure appropriate limits

## Security Updates

Security updates will be announced via:
- GitHub Security Advisories
- Release notes
- Email to registered users (if applicable)

## Disclosure Policy

- We follow responsible disclosure practices
- Vulnerabilities will be disclosed after a fix is available
- Credit will be given to reporters (if desired)


