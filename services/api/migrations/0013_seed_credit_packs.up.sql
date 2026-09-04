INSERT INTO credit_packs (id, name, minutes, price_minor, currency, active)
VALUES
    ('9f1c6d1a-0001-4a00-8a00-000000000064', 'Starter 100', 100, 15000, 'UGX', true),
    ('9f1c6d1a-0002-4a00-8a00-0000000001f4', 'Team 500', 500, 60000, 'UGX', true),
    ('9f1c6d1a-0003-4a00-8a00-0000000007d0', 'Business 2000', 2000, 200000, 'UGX', true)
ON CONFLICT (id) DO NOTHING;
