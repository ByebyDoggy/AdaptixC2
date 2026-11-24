#ifndef ADAPTIXCLIENT_DIALOGAUTOMATION_H
#define ADAPTIXCLIENT_DIALOGAUTOMATION_H

#include <main.h>

class AuthProfile;

// DialogAutomation is the automation control panel. It is split into sections
// (rules, templates, channels, shell scripts) so each concern has its own page
// instead of everything sharing one table.
class DialogAutomation : public QWidget
{
Q_OBJECT

    AuthProfile* profile = nullptr;

    QListWidget*    navList     = nullptr;
    QStackedWidget* stack       = nullptr;
    QLabel*         statusLabel = nullptr;
    QPushButton*    refreshButton = nullptr;
    QPushButton*    closeButton   = nullptr;

    // Rules page
    QTableView*         rulesView  = nullptr;
    QStandardItemModel* rulesModel = nullptr;
    QPushButton*        editRuleButton = nullptr;

    // Templates pages
    QTableView*         artexView   = nullptr;
    QStandardItemModel* artexModel  = nullptr;
    QTableView*         notifyView  = nullptr;
    QStandardItemModel* notifyModel = nullptr;

    // Channels page
    QTableView*         channelView  = nullptr;
    QStandardItemModel* channelModel = nullptr;

    // Script library page (read-only overview)
    QTableView*         scriptView  = nullptr;
    QStandardItemModel* scriptModel = nullptr;

    QJsonArray rulesData;
    QJsonArray artexTemplatesData;
    QJsonArray notifyTemplatesData;
    QJsonArray channelsData;

    bool updating = false;

    void createUI();
    QWidget* createRulesPage();
    QWidget* createArtexPage();
    QWidget* createNotifyPage();
    QWidget* createChannelsPage();
    QWidget* createScriptsPage();

    void setStatus(const QString &text, bool error);
    void applyRules(const QJsonArray &rules, const QJsonArray &channels);
    void applyTemplates(const QJsonObject &payload);
    void applyChannels(const QJsonArray &channels);
    void applyScripts(const QJsonArray &scripts);

    int  currentRow(QTableView* view, QStandardItemModel* model) const;
    void showRuleEditor(int row);
    void showTemplateEditor(const QString &kind, const QString &id, const QJsonObject &existing);
    void showChannelEditor(const QString &channelId, const QString &channelName, const QString &secretLabel);

public:
    explicit DialogAutomation(AuthProfile* authProfile, QWidget* parent = nullptr);
    ~DialogAutomation() override;

public Q_SLOTS:
    void onRefresh();
    void onRefreshTemplates();
    void onRefreshChannels();
    void onRefreshScripts();
    void onEditRule();
    void onNewArtexTemplate();
    void onEditArtexTemplate();
    void onDeleteArtexTemplate();
    void onNewNotifyTemplate();
    void onEditNotifyTemplate();
    void onDeleteNotifyTemplate();
    void onConfigureChannel();
    void onItemChanged(QStandardItem* item);
};

#endif
