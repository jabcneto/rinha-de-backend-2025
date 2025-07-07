-- Inicialização do banco de dados com schema completo incluindo campos de retry
CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    correlation_id UUID NOT NULL UNIQUE,
    amount DECIMAL(10,2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    processor_type VARCHAR(20),
    requested_at TIMESTAMP NOT NULL,
    processed_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    -- Campos de retry
    retry_count INTEGER DEFAULT 0,
    next_retry_at TIMESTAMP NULL,
    last_error TEXT NULL
);

-- Criar tabela de resumo por processador
CREATE TABLE IF NOT EXISTS payment_summaries (
    processor_type VARCHAR(20) PRIMARY KEY,
    total_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    payment_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Inserir registros iniciais para os processadores
INSERT INTO payment_summaries (processor_type, total_amount, payment_count)
VALUES
    ('default', 0, 0),
    ('fallback', 0, 0)
ON CONFLICT (processor_type) DO NOTHING;

-- Índices para performance
CREATE INDEX IF NOT EXISTS idx_payments_correlation_id ON payments (correlation_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments (status);
CREATE INDEX IF NOT EXISTS idx_payments_processor_type ON payments (processor_type);
CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments (created_at);

-- Índices específicos para retry processing
CREATE INDEX IF NOT EXISTS idx_payments_retry_ready
ON payments (next_retry_at)
WHERE next_retry_at IS NOT NULL AND next_retry_at <= NOW();

CREATE INDEX IF NOT EXISTS idx_payments_retry_count
ON payments (retry_count)
WHERE retry_count > 0;

-- Função para atualizar updated_at automaticamente
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger para atualizar updated_at automaticamente
CREATE TRIGGER update_payments_updated_at
    BEFORE UPDATE ON payments
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_payment_summaries_updated_at
    BEFORE UPDATE ON payment_summaries
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
