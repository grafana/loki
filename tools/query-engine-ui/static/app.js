// Initialize mermaid

function App() {
    const [logQL, setLogQL] = React.useState('');
    const [plans, setPlans] = React.useState(null);
    const [error, setError] = React.useState('');
    const [loading, setLoading] = React.useState(false);

    React.useEffect(() => {
        if (plans) {
            renderMermaidDiagrams();
        }
    }, [plans]);

    const renderMermaidDiagrams = () => {
        mermaid.run({
            querySelector: '.mermaid',
        });
    };

    const handleSubmit = async () => {
        if (!logQL.trim()) {
            setError('Please enter a LogQL query');
            return;
        }

        setLoading(true);
        setError('');
        setPlans(null);

        try {
            const response = await fetch('/api/query', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ logql: logQL }),
            });

            const data = await response.json();
            
            if (data.error) {
                setError(data.error);
            } else {
                setPlans(data);
            }
        } catch (err) {
            setError('Failed to process query: ' + err.message);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="container">
            <h1>LogQL Query Engine UI</h1>
            
            <div className="input-container">
                <label htmlFor="logql-input">Enter LogQL Query:</label>
                <textarea 
                    id="logql-input"
                    value={logQL}
                    onChange={(e) => setLogQL(e.target.value)}
                    placeholder="Enter your LogQL query here..."
                />
                <button onClick={handleSubmit} disabled={loading}>
                    {loading ? 'Processing...' : 'Submit Query'}
                </button>
            </div>

            {error && <div className="error">{error}</div>}
            {loading && <div className="loading">Processing query...</div>}

            {plans && (
                <div className="plans-container">
                    <div className="plan-box">
                        <div className="plan-title">Logical Plan</div>
                        <div className="mermaid">{plans.logicalPlan}</div>
                    </div>
                    
                    <div className="plan-box">
                        <div className="plan-title">Physical Plan</div>
                        <div className="mermaid">{plans.physicalPlan}</div>
                    </div>
                </div>
            )}
        </div>
    );
}

const root = ReactDOM.createRoot(document.getElementById('root'));
root.render(<App />);
