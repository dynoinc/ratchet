-- name: CreateRunbook :one
INSERT INTO
    incident_runbooks (attrs)
VALUES
    (@attrs) RETURNING id;

-- name: GetRunbook :one
SELECT
    id,
    attrs
FROM
    incident_runbooks
WHERE
    attrs ->> 'service_name' = @service_name :: text
    AND attrs ->> 'alert_name' = @alert_name :: text
ORDER BY
    id DESC
LIMIT
    1;