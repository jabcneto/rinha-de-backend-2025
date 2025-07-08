-- Criação do banco de dados de pagamentos
-- Rinha de Backend 2025 - Versão Limpa

-- Extensões necessárias
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Tabela principal de pagamentos
CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    correlation_id UUID NOT NULL UNIQUE,
    amount DECIMAL(10,2) NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    processor_type VARCHAR(20) NOT NULL DEFAULT 'default',
    requested_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT
);

-- Tabela de resumo agregado para performance
CREATE TABLE IF NOT EXISTS payment_summary (
    processor_type VARCHAR(20) PRIMARY KEY,
    total_requests BIGINT NOT NULL DEFAULT 0,
    total_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,
    last_updated TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Índices para performance
CREATE INDEX IF NOT EXISTS idx_payments_correlation_id ON payments(correlation_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
CREATE INDEX IF NOT EXISTS idx_payments_processor_type ON payments(processor_type);
CREATE INDEX IF NOT EXISTS idx_payments_requested_at ON payments(requested_at);
CREATE INDEX IF NOT EXISTS idx_payments_retry ON payments(next_retry_at) WHERE next_retry_at IS NOT NULL;

-- Trigger para atualizar updated_at automaticamente
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_payments_updated_at
    BEFORE UPDATE ON payments
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Inserir registros iniciais na tabela de resumo
INSERT INTO payment_summary (processor_type, total_requests, total_amount)
VALUES
    ('default', 0, 0.00),
    ('fallback', 0, 0.00)
ON CONFLICT (processor_type) DO NOTHING;

-- Função para atualizar resumo de pagamentos
CREATE OR REPLACE FUNCTION update_payment_summary(p_processor_type VARCHAR(20), p_amount DECIMAL(10,2))
RETURNS VOID AS $$
BEGIN
    INSERT INTO payment_summary (processor_type, total_requests, total_amount, last_updated)
    VALUES (p_processor_type, 1, p_amount, NOW())
    ON CONFLICT (processor_type)
    DO UPDATE SET
        total_requests = payment_summary.total_requests + 1,
        total_amount = payment_summary.total_amount + p_amount,
        last_updated = NOW();
END;
$$ LANGUAGE plpgsql;
