import pandas as pd
from sklearn.ensemble import IsolationForest
from sklearn.preprocessing import LabelEncoder
import joblib

print("Inizio dello script di addestramento...")

# 1. Caricamento e Preparazione dei Dati
# I nomi delle colonne sono standard per il dataset NSL-KDD
col_names = ["duration", "protocol_type", "service", "flag", "src_bytes", "dst_bytes", "land", "wrong_fragment",
             "urgent", "hot", "num_failed_logins", "logged_in", "num_compromised", "root_shell", "su_attempted",
             "num_root", "num_file_creations", "num_shells", "num_access_files", "num_outbound_cmds",
             "is_host_login", "is_guest_login", "count", "srv_count", "serror_rate", "srv_serror_rate",
             "rerror_rate", "srv_rerror_rate", "same_srv_rate", "diff_srv_rate", "srv_diff_host_rate",
             "dst_host_count", "dst_host_srv_count", "dst_host_same_srv_rate", "dst_host_diff_srv_rate",
             "dst_host_same_src_port_rate", "dst_host_srv_diff_host_rate", "dst_host_serror_rate",
             "dst_host_srv_serror_rate", "dst_host_rerror_rate", "dst_host_srv_rerror_rate", "label", "difficulty"]

# Carichiamo il dataset, usando il parametro 'comment' per ignorare l'header ARFF
# Le righe che iniziano con '@' verranno trattate come commenti e saltate.
df = pd.read_csv("KDDTrain+.txt", header=None, names=col_names, comment='@', low_memory=False)
print(f"Dataset caricato. Numero di righe: {len(df)}")

# Rimuoviamo le ultime due colonne ('label' e 'difficulty') che non servono per l'addestramento unsupervised
df = df.drop(columns=['label', 'difficulty'])

# Gestione delle colonne categoriche: il modello accetta solo numeri.
# Usiamo LabelEncoder per trasformare le stringhe (es. 'tcp', 'http') in numeri.
categorical_cols = ['protocol_type', 'service', 'flag']
for col in categorical_cols:
    le = LabelEncoder()
    df[col] = le.fit_transform(df[col])

# Assicuriamoci che tutti i dati siano numerici
df = df.apply(pd.to_numeric)
print("Dati pre-processati. Colonne categoriche convertite in numeri.")


# 2. Addestramento del Modello Isolation Forest
model = IsolationForest(n_estimators=100, contamination='auto', random_state=42, n_jobs=-1)

print("Inizio addestramento del modello Isolation Forest... (potrebbe richiedere un minuto)")
model.fit(df)
print("Addestramento completato.")

# 3. Salvataggio del Modello Addestrato
model_filename = '../services/inference/isolation_forest_model.joblib'
joblib.dump(model, model_filename)
print(f"Modello salvato con successo come '{model_filename}'")